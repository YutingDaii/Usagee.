package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"interaction/internal/biz"  // 标准导入路径
	"interaction/internal/conf" // 标准导入路径

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-redis/redis/v8"
)


type interactionRepo struct {
	data *Data // 数据库和Redis客户端访问
	log  *log.Helper
	// conf *conf.Data // 如果需要特定的数据配置
}

// NewInteractionRepo 创建交互数据的仓库。
func NewInteractionRepo(data *Data, logger log.Logger /*, conf *conf.Data*/) biz.InteractionRepo {
	return &interactionRepo{
		data: data,
		log:  log.NewHelper(log.With(logger, "module", "data/interaction")),
		// conf: conf,
	}
}

// 缓存键 (常量，便于管理, 使用 Redis Hash Tag 以支持集群)
const (
	cacheKeyVideoLikesPrefix    = "video:{%d}:likes"        // video:{video_id}:likes -> STRING (计数)
	cacheKeyVideoCommentsPrefix = "video:{%d}:comments:ids" // video:{video_id}:comments:ids:{page} -> LIST (评论ID列表)
	cacheKeyCommentDetailPrefix = "comment:{%d}:detail"     // comment:{comment_id}:detail -> HASH 或 STRING (JSON格式)
	cacheKeyUserFavoritesPrefix = "user:{%d}:favorites"     // user:{user_id}:favorites -> ZSET (video_id score:时间戳)
)

// --- 点赞操作 ---

func (r *interactionRepo) CreateLike(ctx context.Context, item *biz.LikeItem) error {
	like := &Like{
		UserID:    item.UserID,
		VideoID:   item.VideoID,
		CreatedAt: item.LikedAt.Unix(),
	}
	// TODO: 实现 'likes' 表基于 video_id % N 的分片逻辑
	// 当前使用默认数据库客户端。
	if err := r.data.db.WithContext(ctx).Create(like).Error; err != nil {
		r.log.WithContext(ctx).Errorf("CreateLike DB error: %v", err)
		return err
	}

	// 更新缓存：INCR 点赞计数
	cacheKey := fmt.Sprintf(cacheKeyVideoLikesPrefix, item.VideoID)
	_, err := r.data.rdb.Incr(ctx, cacheKey).Result()
	if err != nil {
		r.log.WithContext(ctx).Warnf("CreateLike: failed to INCR cache for %s: %v", cacheKey, err)
		// 非致命错误，数据库是最终数据源。可考虑重试或清理。
	} else {
		// 设置/更新过期时间
		r.data.rdb.Expire(ctx, cacheKey, 1*time.Hour) // 根据文档：1小时，访问时续期
	}
	return nil
}

func (r *interactionRepo) DeleteLike(ctx context.Context, userID, videoID int64) error {
	// TODO: 'likes' 表分片
	if err := r.data.db.WithContext(ctx).Where("user_id = ? AND video_id = ?", userID, videoID).Delete(&Like{}).Error; err != nil {
		r.log.WithContext(ctx).Errorf("DeleteLike DB error: %v", err)
		return err
	}

	// 更新缓存：DECR 点赞计数
	cacheKey := fmt.Sprintf(cacheKeyVideoLikesPrefix, videoID)
	_, err := r.data.rdb.Decr(ctx, cacheKey).Result()
	if err != nil {
		r.log.WithContext(ctx).Warnf("DeleteLike: failed to DECR cache for %s: %v", cacheKey, err)
	} else {
		r.data.rdb.Expire(ctx, cacheKey, 1*time.Hour)
	}
	return nil
}

func (r *interactionRepo) GetVideoLikeCount(ctx context.Context, videoID int64) (int64, error) {
	cacheKey := fmt.Sprintf(cacheKeyVideoLikesPrefix, videoID)

	// 1. 尝试从 Redis 读取
	countStr, err := r.data.rdb.Get(ctx, cacheKey).Result()
	if err == nil {
		count, convErr := strconv.ParseInt(countStr, 10, 64)
		if convErr == nil {
			r.data.rdb.Expire(ctx, cacheKey, 1*time.Hour) // 访问时续期 TTL
			return count, nil
		}
		r.log.WithContext(ctx).Warnf("GetVideoLikeCount: failed to parse count from cache '%s': %v", countStr, convErr)
	} else if err != redis.Nil {
		r.log.WithContext(ctx).Warnf("GetVideoLikeCount: Redis GET error for %s: %v", cacheKey, err)
		// 继续从数据库读取，但记录 Redis 错误。
	}

	// 2. 缓存未命中或 Redis 错误：从数据库读取
	// TODO: 如有必要，实现分布式锁以防止缓存雪崩。
	// 为简单起见，暂时直接查询数据库。
	var dbCount int64
	// TODO: 'likes' 表分片
	if err := r.data.db.WithContext(ctx).Model(&Like{}).Where("video_id = ?", videoID).Count(&dbCount).Error; err != nil {
		r.log.WithContext(ctx).Errorf("GetVideoLikeCount: DB count error for video %d: %v", videoID, err)
		return 0, err
	}

	// 3. 写回 Redis
	err = r.data.rdb.Set(ctx, cacheKey, dbCount, 1*time.Hour).Err()
	if err != nil {
		r.log.WithContext(ctx).Warnf("GetVideoLikeCount: failed to SET cache for %s: %v", cacheKey, err)
	}
	return dbCount, nil
}

func (r *interactionRepo) IsLiked(ctx context.Context, userID, videoID int64) (bool, error) {
	var count int64
	// TODO: 'likes' 表分片
	err := r.data.db.WithContext(ctx).Model(&Like{}).Where("user_id = ? AND video_id = ?", userID, videoID).Count(&count).Error
	if err != nil {
		r.log.WithContext(ctx).Errorf("IsLiked DB error: %v", err)
		return false, err
	}
	return count > 0, nil
}

// --- 评论操作 ---

func (r *interactionRepo) CreateComment(ctx context.Context, item *biz.CommentItem) (*biz.CommentItem, error) {
	// 关于评论ID生成：
	// - 如果CommentID是字符串类型(如VARCHAR(36))，则需要在调用Create前生成 (例如使用Snowflake或UUID)。
	// - 如果CommentID是整型且由数据库自增，则路径生成会更复杂，可能需要在插入后更新路径。
	// - 当前实现假设CommentID已在业务层或服务层设置，或者路径已构建。
	// 物化路径 (materialized path) 生成逻辑：
	// - 如果ParentID为0 (根评论)，路径为 "root/{CommentID}"。
	// - 如果ParentID不为0 (回复)，则获取父评论路径，然后追加 "/{CommentID}"。
	// - 这要求CommentID在调用此函数前已知，或在此处生成。

	dbComment := &Comment{
		ID:        item.CommentID, // 如果是字符串类型，必须唯一
		VideoID:   item.VideoID,
		UserID:    item.UserID,
		Content:   item.Content,
		ParentID:  item.ParentID,
		CreatedAt: item.CreatedAt.Unix(),
		Path:      item.Path, // 路径应在业务层调用仓库前构建好
	}

	
	if dbComment.ID == 0 { // 如果ID是整型且由数据库自增，这是一个占位符
		r.log.WithContext(ctx).Warnf("CreateComment: CommentID is 0, path generation might be incomplete if ID is auto-generated by DB.")
	}

	if dbComment.ParentID == 0 {
		dbComment.Path = fmt.Sprintf("root/%d", dbComment.ID) // 假设ID已知
	} else {
		// 获取父评论以得到其路径
		parentDBComment, err := r.getDBCommentByID(ctx, dbComment.ParentID) // 内部辅助函数
		if err != nil {
			r.log.WithContext(ctx).Errorf("CreateComment: failed to get parent comment %d for path: %v", dbComment.ParentID, err)
			return nil, fmt.Errorf("parent comment not found for path generation: %w", err)
		}
		dbComment.Path = fmt.Sprintf("%s/%d", parentDBComment.Path, dbComment.ID)
	}


	// TODO: 'comments' 表基于 video_id % N 的分片逻辑
	if err := r.data.db.WithContext(ctx).Create(dbComment).Error; err != nil {
		r.log.WithContext(ctx).Errorf("CreateComment DB error: %v", err)
		return nil, err
	}
	item.CommentID = dbComment.ID // 确保业务模型拥有最终ID
	item.Path = dbComment.Path     // 确保业务模型拥有最终路径

	// 评论的缓存失效/更新策略:
	// - 新评论: 如果已缓存，则添加到第一页列表的开头，并移除最后一个ID。
	// - 缓存新的评论实体本身。

	// 1. 缓存评论实体
	commentCacheKey := fmt.Sprintf(cacheKeyCommentDetailPrefix, item.CommentID)
	commentJSON, _ := json.Marshal(item) // 序列化 biz.CommentItem
	err := r.data.rdb.Set(ctx, commentCacheKey, commentJSON, 30*time.Minute).Err()
	if err != nil {
		r.log.WithContext(ctx).Warnf("CreateComment: failed to cache comment detail %s: %v", commentCacheKey, err)
	}

	// 2. 更新相关评论列表 (例如，视频的第一页)
	// 这很复杂，因为它取决于这是根评论还是回复，以及具体是哪一页。
	// 为简单起见，我们可能只让视频或父评论的第一页缓存失效。
	// 或者，如果我们只缓存根评论ID，则更新该列表。
	// 设计文档提到“追加到第一页列表”。
	// 假设我们正在缓存视频的根评论ID。
	if item.ParentID == 0 {
		listCacheKey := fmt.Sprintf(cacheKeyVideoCommentsPrefix+":%d", item.VideoID, 1) // 第1页
		// 添加到列表开头，然后修剪。这需要知道页面大小。
		// 目前，仅删除以强制刷新。更简单，但非最优。
		r.data.rdb.Del(ctx, listCacheKey)
		r.log.WithContext(ctx).Infof("CreateComment: invalidated comment list cache %s", listCacheKey)
	} else {
		// 如果是回复，我们可能让父评论的回复缓存失效。
		// 这需要为回复设置不同的缓存键结构，例如：comment:replies:ids:{parent_id}:{page}
		// 目前未明确处理回复列表的缓存更新。
	}

	return item, nil
}


// getDBCommentByID 是一个内部辅助函数，用于从数据库获取原始的 Comment。
func (r *interactionRepo) getDBCommentByID(ctx context.Context, commentID int64) (*Comment, error) {
	var dbComment Comment
	// TODO: 'comments' 表分片
	if err := r.data.db.WithContext(ctx).First(&dbComment, commentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("comment with ID %d not found: %w", commentID, biz.ErrNotFound) // 在 biz 包中定义 ErrNotFound
		}
		return nil, err
	}
	return &dbComment, nil
}


func (r *interactionRepo) GetCommentByID(ctx context.Context, commentID int64) (*biz.CommentItem, error) {
	// 1. 尝试缓存
	cacheKey := fmt.Sprintf(cacheKeyCommentDetailPrefix, commentID)
	commentJSON, err := r.data.rdb.Get(ctx, cacheKey).Result()
	if err == nil {
		var item biz.CommentItem
		if json.Unmarshal([]byte(commentJSON), &item) == nil {
			r.data.rdb.Expire(ctx, cacheKey, 30*time.Minute) // 续期 TTL
			return &item, nil
		}
		r.log.WithContext(ctx).Warnf("GetCommentByID: failed to unmarshal comment from cache %s", cacheKey)
	} else if err != redis.Nil {
		r.log.WithContext(ctx).Warnf("GetCommentByID: Redis GET error for %s: %v", cacheKey, err)
	}

	// 2. 数据库回源
	dbComment, err := r.getDBCommentByID(ctx, commentID)
	if err != nil {
		return nil, err // 已在 getDBCommentByID 中记录日志
	}

	bizComment := &biz.CommentItem{
		CommentID: dbComment.ID,
		VideoID:   dbComment.VideoID,
		UserID:    dbComment.UserID,
		Content:   dbComment.Content,
		ParentID:  dbComment.ParentID,
		CreatedAt: time.Unix(dbComment.CreatedAt, 0),
		Path:      dbComment.Path,
		// 如果此函数应填充回复，则需要单独获取。
		// 目前，GetCommentByID 返回单个评论，不包含其直接回复。
	}

	// 3. 缓存结果
	newCommentJSON, _ := json.Marshal(bizComment)
	setErr := r.data.rdb.Set(ctx, cacheKey, newCommentJSON, 30*time.Minute).Err()
	if setErr != nil {
		r.log.WithContext(ctx).Warnf("GetCommentByID: failed to SET cache for %s: %v", cacheKey, setErr)
	}

	return bizComment, nil
}


func (r *interactionRepo) ListCommentsByVideoID(ctx context.Context, videoID int64, pageNum, pageSize int32) ([]*biz.CommentItem, int64, error) {
	// 此函数列出视频的根评论。回复由 ListRepliesByParentID 处理。
	// 缓存策略：缓存第 N 页的根评论ID列表。
	listCacheKey := fmt.Sprintf(cacheKeyVideoCommentsPrefix+":%d", videoID, pageNum)

	// 1. 尝试缓存ID (根据文档，仅缓存前10页)
	var commentIDs []int64
	var totalComments int64 // 此视频的根评论总数

	if pageNum <= 10 { // 仅缓存前10页
		idsStr, err := r.data.rdb.LRange(ctx, listCacheKey, 0, -1).Result()
		if err == nil && len(idsStr) > 0 {
			// 如果ID已缓存，我们还需要总数。
			// 设计文档在此处有些模糊。假设我们也单独缓存总数或与第1页一起缓存。
			// 为简单起见，如果找到ID，则继续进行MGET，但总数可能已过时或需要另一次查询。
			// 暂时假设如果 listCacheKey 存在，则它是有效的。
			// 如果未缓存，仍需从数据库获取总数。
			// 这部分的缓存比较复杂。

			// 简化：如果找到ID，则MGET它们。如果总数也未缓存，则将从数据库中获取。
			// 这不是理想的方案。更好的缓存应存储 {ids: [...], total: X}

			// 进一步完善：如果请求并缓存了第1页，则它可能也包含 total_count。
			// 为简单起见，目前假设我们总是从数据库获取总数，
			// 并且仅在可用时使用缓存的ID。

			tempIDs := make([]int64, 0, len(idsStr))
			for _, idStr := range idsStr {
				id, _ := strconv.ParseInt(idStr, 10, 64)
				tempIDs = append(tempIDs, id)
			}
			
			// MGET 评论详情
			if len(tempIDs) > 0 {
				comments := make([]*biz.CommentItem, 0, len(tempIDs))
				// 从评论详情缓存中 MGET
				keysToMGet := make([]string, len(tempIDs))
				for i, id := range tempIDs {
					keysToMGet[i] = fmt.Sprintf(cacheKeyCommentDetailPrefix, id)
				}
				cachedDetails, mgetErr := r.data.rdb.MGet(ctx, keysToMGet...).Result()
				if mgetErr == nil {
					allFoundInMGet := true
					for i, detailResult := range cachedDetails {
						if detailResult == nil {
							allFoundInMGet = false
							break // 至少有一个评论详情不在缓存中
						}
						var item biz.CommentItem
						if err := json.Unmarshal([]byte(detailResult.(string)), &item); err == nil {
							comments = append(comments, &item)
						} else {
							allFoundInMGet = false; break // 反序列化错误
						}
					}
					if allFoundInMGet && len(comments) == len(tempIDs) {
						// 成功从缓存中获取所有数据。需要总数。
						// 这里比较棘手。假设我们仍然从数据库获取总数。
						// 这不是一个好的缓存策略。
						// 为安全起见，暂时回退到数据库逻辑。
						r.log.WithContext(ctx).Infof("ListCommentsByVideoID: MGET successful for %s, but falling to DB for consistency and total count.", listCacheKey)
					}
				}
			}
		} else if err != redis.Nil {
			r.log.WithContext(ctx).Warnf("ListCommentsByVideoID: Redis LRANGE error for %s: %v", listCacheKey, err)
		}
	}

	// 2. 数据库回源 (或如果缓存部分/不一致)
	var dbComments []*Comment
	offset := (pageNum - 1) * pageSize
	// TODO: 'comments' 表分片
	query := r.data.db.WithContext(ctx).Model(&Comment{}).
		Where("video_id = ? AND parent_id = ?", videoID, 0). // 仅根评论
		Order("created_at DESC")

	// 获取此视频的根评论总数
	if err := query.Count(&totalComments).Error; err != nil {
		r.log.WithContext(ctx).Errorf("ListCommentsByVideoID: DB count error for video %d: %v", videoID, err)
		return nil, 0, err
	}

	if err := query.Limit(int(pageSize)).Offset(int(offset)).Find(&dbComments).Error; err != nil {
		r.log.WithContext(ctx).Errorf("ListCommentsByVideoID: DB find error for video %d: %v", videoID, err)
		return nil, 0, err
	}

	bizComments := make([]*biz.CommentItem, len(dbComments))
	commentIDsToCache := make([]string, len(dbComments)) // 用于列表缓存
	commentsToCache := make(map[string]interface{})    // 用于详情缓存 MSET

	for i, dbComment := range dbComments {
		bizComments[i] = &biz.CommentItem{
			CommentID: dbComment.ID,
			VideoID:   dbComment.VideoID,
			UserID:    dbComment.UserID,
			Content:   dbComment.Content,
			ParentID:  dbComment.ParentID,
			CreatedAt: time.Unix(dbComment.CreatedAt, 0),
			Path:      dbComment.Path,
			// 此处不填充回复；如果需要，应按需获取。
		}
		commentIDsToCache[i] = strconv.FormatInt(dbComment.ID, 10)
		commentJSON, _ := json.Marshal(bizComments[i])
		commentsToCache[fmt.Sprintf(cacheKeyCommentDetailPrefix, dbComment.ID)] = commentJSON
	}

	// 3. 缓存结果
	if pageNum <= 10 && len(commentIDsToCache) > 0 {
		// 缓存ID列表
		pipe := r.data.rdb.Pipeline()
		pipe.Del(ctx, listCacheKey) // 删除旧列表
		pipe.RPush(ctx, listCacheKey, commentIDsToCache) // 添加新列表
		pipe.Expire(ctx, listCacheKey, 30*time.Minute)
		// 缓存评论详情 (MSET)
		if len(commentsToCache) > 0 {
			pipe.MSet(ctx, commentsToCache)
			// 评论详情的单独TTL在通过GetCommentByID获取或创建时设置。
			// 或者，也在此处设置。
			for key := range commentsToCache {
				pipe.Expire(ctx, key, 30*time.Minute)
			}
		}
		_, err := pipe.Exec(ctx)
		if err != nil {
			r.log.WithContext(ctx).Warnf("ListCommentsByVideoID: failed to cache comment list/details for %s: %v", listCacheKey, err)
		}
	}
	return bizComments, totalComments, nil
}


func (r *interactionRepo) ListRepliesByParentID(ctx context.Context, parentID int64, pageNum, pageSize int32) ([]*biz.CommentItem, int64, error) {
    // 此函数列出对给定父评论的直接回复。
    // 回复的缓存可能很复杂。设计文档提到“顶级评论与回复分开缓存”。
    // 假设缓存键类似于：comment:replies:ids:{parent_id}:{page}
    // 为简单起见，此示例将主要使用数据库和基本的实体缓存。

    var dbReplies []*Comment
    var totalReplies int64
    offset := (pageNum - 1) * pageSize

    // TODO: 'comments' 表分片 (基于 video_id，父评论应包含此信息)
    // 需要获取父评论以了解其 video_id 以进行分片，或者如果 parentID 全局唯一，则假设 parentID 足够。
    query := r.data.db.WithContext(ctx).Model(&Comment{}).
        Where("parent_id = ?", parentID).
        Order("created_at ASC") // 回复通常按时间顺序显示

    if err := query.Count(&totalReplies).Error; err != nil {
        r.log.WithContext(ctx).Errorf("ListRepliesByParentID: DB count error for parent %d: %v", parentID, err)
        return nil, 0, err
    }

    if err := query.Limit(int(pageSize)).Offset(int(offset)).Find(&dbReplies).Error; err != nil {
        r.log.WithContext(ctx).Errorf("ListRepliesByParentID: DB find error for parent %d: %v", parentID, err)
        return nil, 0, err
    }

    bizReplies := make([]*biz.CommentItem, len(dbReplies))
    for i, dbReply := range dbReplies {
        bizReplies[i] = &biz.CommentItem{
            CommentID: dbReply.ID,
            VideoID:   dbReply.VideoID, // 应与父评论的 video_id 相同
            UserID:    dbReply.UserID,
            Content:   dbReply.Content,
            ParentID:  dbReply.ParentID,
            CreatedAt: time.Unix(dbReply.CreatedAt, 0),
            Path:      dbReply.Path,
            // 此处不填充更深层级的嵌套回复。
        }
        // 缓存单个回复详情
        commentCacheKey := fmt.Sprintf(cacheKeyCommentDetailPrefix, dbReply.ID)
        commentJSON, _ := json.Marshal(bizReplies[i])
        err := r.data.rdb.Set(ctx, commentCacheKey, commentJSON, 30*time.Minute).Err()
        if err != nil {
            r.log.WithContext(ctx).Warnf("ListRepliesByParentID: failed to cache reply detail %s: %v", commentCacheKey, err)
        }
    }
    // TODO: 如果性能需要，实现回复ID列表的缓存。
    return bizReplies, totalReplies, nil
}


// --- 收藏操作 ---

func (r *interactionRepo) CreateFavorite(ctx context.Context, item *biz.FavoriteItem) error {
	fav := &Favorite{
		UserID:    item.UserID,
		VideoID:   item.VideoID,
		CreatedAt: item.FavoritedAt.Unix(),
	}
	// TODO: 'favorites' 表基于 user_id % N 的分片逻辑
	if err := r.data.db.WithContext(ctx).Create(fav).Error; err != nil {
		r.log.WithContext(ctx).Errorf("CreateFavorite DB error: %v", err)
		return err
	}

	// 更新缓存: ZADD 到用户的收藏有序集合
	cacheKey := fmt.Sprintf(cacheKeyUserFavoritesPrefix, item.UserID)
	_, err := r.data.rdb.ZAdd(ctx, cacheKey, &redis.Z{
		Score:  float64(item.FavoritedAt.Unix()),
		Member: item.VideoID, // 将 video_id 作为成员存储
	}).Result()
	if err != nil {
		r.log.WithContext(ctx).Warnf("CreateFavorite: failed to ZADD to cache %s: %v", cacheKey, err)
	} else {
		r.data.rdb.Expire(ctx, cacheKey, 24*time.Hour) // 缓存24小时
	}
	return nil
}

func (r *interactionRepo) DeleteFavorite(ctx context.Context, userID, videoID int64) error {
	// TODO: 'favorites' 表分片
	if err := r.data.db.WithContext(ctx).Where("user_id = ? AND video_id = ?", userID, videoID).Delete(&Favorite{}).Error; err != nil {
		r.log.WithContext(ctx).Errorf("DeleteFavorite DB error: %v", err)
		return err
	}

	// 更新缓存: 从用户的收藏有序集合中 ZREM
	cacheKey := fmt.Sprintf(cacheKeyUserFavoritesPrefix, userID)
	_, err := r.data.rdb.ZRem(ctx, cacheKey, videoID).Result() // 移除 video_id
	if err != nil {
		r.log.WithContext(ctx).Warnf("DeleteFavorite: failed to ZREM from cache %s: %v", cacheKey, err)
	}
	// 过期时间将由 ZADD 或下次完整列表获取时处理。
	return nil
}

func (r *interactionRepo) ListFavoritesByUserID(ctx context.Context, userID int64, pageNum, pageSize int32) ([]*biz.FavoriteItem, int64, error) {
	cacheKey := fmt.Sprintf(cacheKeyUserFavoritesPrefix, userID)
	start := int64(pageNum-1) * int64(pageSize)
	stop := start + int64(pageSize) - 1

	// 1. 尝试从 Redis ZSET 读取
	// ZREVRANGE 获取最新的收藏
	videoIDsWithScores, err := r.data.rdb.ZRevRangeWithScores(ctx, cacheKey, start, stop).Result()
	if err == nil && len(videoIDsWithScores) > 0 {
		favorites := make([]*biz.FavoriteItem, len(videoIDsWithScores))
		for i, z := range videoIDsWithScores {
			videoID, _ := strconv.ParseInt(z.Member.(string), 10, 64)
			favorites[i] = &biz.FavoriteItem{
				UserID:      userID,
				VideoID:     videoID,
				FavoritedAt: time.Unix(int64(z.Score), 0),
			}
		}
		// 从 ZCARD 获取总数
		totalFavorites, cardErr := r.data.rdb.ZCard(ctx, cacheKey).Result()
		if cardErr != nil {
			r.log.WithContext(ctx).Warnf("ListFavoritesByUserID: ZCard error for %s: %v. Falling back to DB.", cacheKey, cardErr)
			// 如果获取总数失败，则回源到数据库
		} else {
			r.data.rdb.Expire(ctx, cacheKey, 24*time.Hour) // 续期 TTL
			return favorites, totalFavorites, nil
		}
	} else if err != redis.Nil {
		r.log.WithContext(ctx).Warnf("ListFavoritesByUserID: Redis ZREVRANGE error for %s: %v", cacheKey, err)
	}

	// 2. 数据库回源
	r.log.WithContext(ctx).Infof("ListFavoritesByUserID: cache miss or error for %s. Fetching from DB.", cacheKey)
	var dbFavorites []*Favorite
	var totalFavoritesDB int64
	// TODO: 'favorites' 表分片
	query := r.data.db.WithContext(ctx).Model(&Favorite{}).Where("user_id = ?", userID)

	if err := query.Count(&totalFavoritesDB).Error; err != nil {
		r.log.WithContext(ctx).Errorf("ListFavoritesByUserID: DB count error for user %d: %v", userID, err)
		return nil, 0, err
	}

	if err := query.Order("created_at DESC").Limit(int(pageSize)).Offset(int(start)).Find(&dbFavorites).Error; err != nil {
		r.log.WithContext(ctx).Errorf("ListFavoritesByUserID: DB find error for user %d: %v", userID, err)
		return nil, 0, err
	}

	bizFavorites := make([]*biz.FavoriteItem, len(dbFavorites))
	redisZAdds := make([]*redis.Z, len(dbFavorites)) // 用于缓存回写

	for i, dbFav := range dbFavorites {
		bizFavorites[i] = &biz.FavoriteItem{
			UserID:      dbFav.UserID,
			VideoID:     dbFav.VideoID,
			FavoritedAt: time.Unix(dbFav.CreatedAt, 0),
		}
		redisZAdds[i] = &redis.Z{
			Score:  float64(dbFav.CreatedAt),
			Member: dbFav.VideoID,
		}
	}

	// 3. 回写到 Redis 缓存 (重建用户的 ZSET)
	// 这是完全重建；如果只是追加可以优化。
	// 为简单起见，清除并 ZADD 所有获取的项目。
	// 更好的完全重建方法是从数据库获取用户的所有收藏，然后缓存。
	// 这里我们只缓存当前页，这对 ZSET 来说不是理想的。
	// 设计暗示 "ZREVRANGE 全量拉取或分页"。如果从 ZSET 分页，则没问题。
	// 如果数据库回源，理想情况下我们应该缓存用户的所有收藏。
	// 目前，仅缓存获取的页面 (对 ZSET 用处不大)。
	// 正确的缓存填充会从数据库获取用户的所有收藏。
	// 如果发生数据库回源，调整为获取所有并缓存所有。

	if len(redisZAdds) > 0 { // 仅当从数据库获取到数据时才缓存
		// 为了正确重建缓存，从数据库获取此用户的所有收藏
		var allDbFavorites []*Favorite
		// TODO: 分片
		if errAll := r.data.db.WithContext(ctx).Model(&Favorite{}).Where("user_id = ?", userID).Order("created_at DESC").Find(&allDbFavorites).Error; errAll == nil {
			allRedisZAdds := make([]*redis.Z, len(allDbFavorites))
			for i, dbFavFull := range allDbFavorites { // 使用新的迭代变量名以避免与外部作用域冲突
				allRedisZAdds[i] = &redis.Z{
					Score:  float64(dbFavFull.CreatedAt),
					Member: dbFavFull.VideoID,
				}
			}
			if len(allRedisZAdds) > 0 {
				pipe := r.data.rdb.Pipeline()
				pipe.Del(ctx, cacheKey) // 清除旧集合
				pipe.ZAdd(ctx, cacheKey, allRedisZAdds...)
				pipe.Expire(ctx, cacheKey, 24*time.Hour)
				_, execErr := pipe.Exec(ctx)
				if execErr != nil {
					r.log.WithContext(ctx).Warnf("ListFavoritesByUserID: failed to rebuild ZSET cache for %s: %v", cacheKey, execErr)
				} else {
					r.log.WithContext(ctx).Infof("ListFavoritesByUserID: rebuilt ZSET cache for user %d with %d items.", userID, len(allRedisZAdds))
				}
			}
		} else {
			r.log.WithContext(ctx).Errorf("ListFavoritesByUserID: failed to fetch all favorites for user %d to rebuild cache: %v", userID, errAll)
		}
	}
	return bizFavorites, totalFavoritesDB, nil
}

func (r *interactionRepo) IsFavorited(ctx context.Context, userID, videoID int64) (bool, error) {
	// 1. 首先检查缓存
	cacheKey := fmt.Sprintf(cacheKeyUserFavoritesPrefix, userID)
	score, err := r.data.rdb.ZScore(ctx, cacheKey, strconv.FormatInt(videoID, 10)).Result()
	if err == nil {
		// 如果 score > 0 (或仅存在)，则在缓存中找到
		return score > 0, nil // 假设 score 是时间戳，所以如果存在则总是 > 0
	}
	if err != redis.Nil {
		r.log.WithContext(ctx).Warnf("IsFavorited: ZScore error for %s, member %d: %v. Falling to DB.", cacheKey, videoID, err)
	}

	// 2. 数据库回源
	var count int64
	// TODO: 'favorites' 表分片
	dbErr := r.data.db.WithContext(ctx).Model(&Favorite{}).Where("user_id = ? AND video_id = ?", userID, videoID).Count(&count).Error
	if dbErr != nil {
		r.log.WithContext(ctx).Errorf("IsFavorited DB error: %v", dbErr)
		return false, dbErr
	}
	// 可选：如果在数据库中找到但在缓存中未找到，则添加到缓存 (尽管 ListFavoritesByUserID 应该会填充它)
	return count > 0, nil
}

// 确保 interactionRepo 实现 biz.InteractionRepo
var _ biz.InteractionRepo = (*interactionRepo)(nil)

// 辅助定义 biz.ErrNotFound，或确保它在 biz 包中定义。
// 目前，假设它存在或使用通用错误。
// 示例：如果未定义 biz.ErrNotFound：
// var ErrNotFound = errors.New("not found")
// 然后使用它。最好在 biz 中定义。
