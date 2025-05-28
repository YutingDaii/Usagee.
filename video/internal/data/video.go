package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"video/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

// 任务类型常量
const (
	//当用户上传原始视频后，需要将视频转换为多种分辨率和码率
	TaskTypeTranscode = "TRANSCODE" // 转码任务
	//当用户上传原始视频后，需要生成视频的缩略图
	TaskTypeThumbnail = "THUMBNAIL" // 缩略图任务
	//当用户上传原始视频后，需要提取视频的元数据
	TaskTypeMetadataExtraction = "METADATA_EXTRACTION" // 元数据提取任务
	//所有任务都从TaskStatusPending(待处理)状态开始，然后经历以下状态变化
	TaskStatusPending = "PENDING" // 待处理状态
)

type videoRepo struct {
	data          *Data
	log           *log.Helper
	kafkaProducer *KafkaProducer
}

// NewVideoRepo .
func NewVideoRepo(data *Data, logger log.Logger, kafkaProducer *KafkaProducer) *videoRepo {
	return &videoRepo{
		data:          data,
		log:           log.NewHelper(logger),
		kafkaProducer: kafkaProducer,
	}
}

// CreateVideo creates a new video record
func (r *videoRepo) CreateVideo(ctx context.Context, v *biz.Video) (*biz.Video, error) {
	if v == nil {
		return nil, errors.New("video cannot be nil")
	}

	// 使用sql.DB插入数据，确保使用预生成的ID
	query := `INSERT INTO videos (id, user_id, title, description, duration, url, resolution, visibility, status, cover_url, created_at, updated_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	stmt, err := r.data.db.PrepareContext(ctx, query)
	if err != nil {
		r.log.Errorf("failed to prepare statement: %v", err)
		return nil, err
	}
	defer stmt.Close()

	now := time.Now()
	v.CreatedAt = now
	v.UpdatedAt = now

	// 执行SQL插入，注意第一个参数现在是预生成的ID
	_, err = stmt.ExecContext(ctx,
		v.ID, // 使用预生成的雪花算法ID
		v.UserID,
		v.Title,
		v.Description,
		v.Duration,
		"", // URL将在转码后更新
		"", // Resolution将在转码后更新
		string(v.Visibility),
		string(v.Status),
		v.CoverURL,
		now,
		now,
	)
	if err != nil {
		r.log.Errorf("failed to create video: %v", err)
		return nil, err
	}

	// 发布Kafka事件
	if err := r.kafkaProducer.PublishVideoCreated(ctx, v); err != nil {
		r.log.Warnf("failed to publish video.created event: %v", err)
		// 继续执行，即使Kafka发布失败
	}

	// 创建处理任务
	if err := r.createProcessingTasks(ctx, v.ID); err != nil {
		r.log.Errorf("failed to create processing tasks: %v", err)
		// 继续返回视频，但记录错误
	}

	return v, nil
}

// 转码
func (r *videoRepo) TranscodeVideo(ctx context.Context, v *biz.Video) ([]biz.VideoVariant, error) {
	// 先查询视频是否存在
	var (
		visibility string
		status     string
	)

	query := `SELECT status, visibility FROM videos WHERE id = ?`
	err := r.data.db.QueryRowContext(ctx, query, v.ID).Scan(&status, &visibility)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("video not found: %v", v.ID)
		}
		return nil, err
	}

	// 更新视频状态为处理中
	updateQuery := `UPDATE videos SET status = ? WHERE id = ?`
	_, err = r.data.db.ExecContext(ctx, updateQuery, string(biz.ProcessingStatus), v.ID)
	if err != nil {
		return nil, err
	}

	// 在实际实现中，这里会调用外部转码服务
	// 这里仅演示创建示例变体
	variants := []biz.VideoVariant{
		{
			Resolution: "720p",
			Codec:      "H264",
			Bitrate:    2500,
			StorageURL: fmt.Sprintf("https://cdn.example.com/videos/%d/720p.mp4", v.ID),
		},
		{
			Resolution: "1080p",
			Codec:      "H264",
			Bitrate:    5000,
			StorageURL: fmt.Sprintf("https://cdn.example.com/videos/%d/1080p.mp4", v.ID),
		},
	}

	// 保存变体到数据库
	for _, variant := range variants {
		insertVariant := `INSERT INTO video_variants (video_id, resolution, codec, bitrate, url, created_at)
						 VALUES (?, ?, ?, ?, ?, ?)`
		_, err = r.data.db.ExecContext(ctx, insertVariant,
			v.ID,
			variant.Resolution,
			variant.Codec,
			variant.Bitrate,
			variant.StorageURL,
			time.Now(),
		)
		if err != nil {
			r.log.Errorf("failed to save variant: %v", err)
			continue
		}
	}

	// 更新视频状态为就绪，并设置默认分辨率和URL
	updateVideoQuery := `UPDATE videos SET status = ?, url = ?, resolution = ? WHERE id = ?`
	_, err = r.data.db.ExecContext(ctx, updateVideoQuery,
		string(biz.ReadyStatus),
		variants[0].StorageURL,
		variants[0].Resolution,
		v.ID,
	)
	if err != nil {
		return nil, err
	}

	// 更新原始视频对象的状态和变体
	v.Status = biz.ReadyStatus
	v.Variants = variants

	return variants, nil
}

// GenerateThumbnail creates a thumbnail for the video
func (r *videoRepo) GenerateThumbnail(ctx context.Context, v *biz.Video) (string, error) {
	// 检查视频是否存在
	query := `SELECT id FROM videos WHERE id = ?`
	err := r.data.db.QueryRowContext(ctx, query, v.ID).Scan(&v.ID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("video not found: %v", v.ID)
		}
		return "", err
	}

	// 在实际实现中，这里会调用服务生成缩略图
	// 这里仅演示创建示例缩略图URL
	thumbnailURL := fmt.Sprintf("https://cdn.example.com/thumbnails/%d.jpg", v.ID)

	// 更新视频缩略图URL
	updateQuery := `UPDATE videos SET cover_url = ? WHERE id = ?`
	_, err = r.data.db.ExecContext(ctx, updateQuery, thumbnailURL, v.ID)
	if err != nil {
		return "", err
	}

	// 更新原始视频对象的缩略图URL
	v.CoverURL = thumbnailURL

	return thumbnailURL, nil
}

// GetVideo retrieves a video by ID
func (r *videoRepo) GetVideo(ctx context.Context, id int64) (*biz.Video, error) {
	// 使用{}确保相同videoID的所有键都映射到同一节点
	videoKey := fmt.Sprintf("video:{%d}", id) // 使用{}定义hash tag

	// 使用Redis集群客户端获取缓存
	videoJSON, err := r.data.rdb.Get(ctx, videoKey).Result()
	if err == nil {
		var video biz.Video
		if err := json.Unmarshal([]byte(videoJSON), &video); err == nil {
			return &video, nil
		}
	}
	//解析videoJson为biz.Video
	if videoJSON != "" {
		var cachedVideo biz.Video
		err := json.Unmarshal([]byte(videoJSON), &cachedVideo)
		if err == nil {
			return &cachedVideo, nil
		}
		// 解析失败则记录日志，继续从数据库获取
		r.log.Errorf("failed to unmarshal cached video: %v", err)
	}

	// 从数据库获取视频信息
	var video biz.Video
	var (
		visibility string
		status     string
		createdAt  time.Time
		updatedAt  time.Time
		coverURL   sql.NullString
	)

	query := `SELECT id, user_id, title, description, duration, visibility, status, 
             COALESCE(cover_url, '') as cover_url, created_at, updated_at 
             FROM videos WHERE id = ?`

	err = r.data.db.QueryRowContext(ctx, query, id).Scan(
		&video.ID,
		&video.UserID,
		&video.Title,
		&video.Description,
		&video.Duration,
		&visibility,
		&status,
		&coverURL,
		&createdAt,
		&updatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("video not found: %v", id)
		}
		return nil, err
	}

	// 设置视频属性
	video.Visibility = biz.VisibilityType(visibility)
	video.Status = biz.VideoStatus(status)
	if coverURL.Valid {
		video.CoverURL = coverURL.String
	}
	video.CreatedAt = createdAt
	video.UpdatedAt = updatedAt

	// 获取视频变体
	variantsQuery := `SELECT resolution, codec, bitrate, url FROM video_variants WHERE video_id = ?`
	rows, err := r.data.db.QueryContext(ctx, variantsQuery, id)
	if err != nil {
		r.log.Errorf("failed to get video variants: %v", err)
		// 继续执行，即使没有变体
	} else {
		defer rows.Close()

		for rows.Next() {
			var variant biz.VideoVariant
			if err := rows.Scan(&variant.Resolution, &variant.Codec, &variant.Bitrate, &variant.StorageURL); err != nil {
				r.log.Errorf("failed to scan variant: %v", err)
				continue
			}
			video.Variants = append(video.Variants, variant)
		}
	}

	// 缓存视频元数据到Redis
	r.cacheVideoMetadata(ctx, &video)

	return &video, nil
}

// DeleteVideo marks a video as deleted
func (r *videoRepo) DeleteVideo(ctx context.Context, id int64) error {
	// 获取视频信息
	var userID int64
	query := `SELECT user_id FROM videos WHERE id = ?`
	err := r.data.db.QueryRowContext(ctx, query, id).Scan(&userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("video not found: %v", id)
		}
		return err
	}

	// 更新视频状态为已删除
	updateQuery := `UPDATE videos SET status = ?, updated_at = ? WHERE id = ?`
	_, err = r.data.db.ExecContext(ctx, updateQuery, string(biz.DeletedStatus), time.Now(), id)
	if err != nil {
		return err
	}

	// 构建用于事件发布的视频对象
	video := &biz.Video{
		ID:     id,
		UserID: userID,
		Status: biz.DeletedStatus,
	}

	// 发布视频删除事件
	if err := r.kafkaProducer.PublishVideoDeleted(ctx, video); err != nil {
		r.log.Warnf("failed to publish video.deleted event: %v", err)
		// 继续执行，即使Kafka发布失败
	}

	// 从Redis缓存删除
	videoKey := fmt.Sprintf("video:{%d}", id)
	r.data.rdb.Del(ctx, videoKey)

	return nil
}

// GetVideoByUserID retrieves videos for a specific user
func (r *videoRepo) GetVideoByUserID(ctx context.Context, userID int64) ([]*biz.Video, error) {
	// 查询用户的所有未删除视频
	query := `SELECT id, user_id, title, description, duration, visibility, status, 
             COALESCE(cover_url, '') as cover_url, created_at, updated_at 
             FROM videos 
             WHERE user_id = ? AND status != ?
             ORDER BY created_at DESC`

	rows, err := r.data.db.QueryContext(ctx, query, userID, string(biz.DeletedStatus))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var videos []*biz.Video
	for rows.Next() {
		var video biz.Video
		var (
			visibility string
			status     string
			coverURL   string
		)

		if err := rows.Scan(
			&video.ID,
			&video.UserID,
			&video.Title,
			&video.Description,
			&video.Duration,
			&visibility,
			&status,
			&coverURL,
			&video.CreatedAt,
			&video.UpdatedAt,
		); err != nil {
			r.log.Errorf("failed to scan video: %v", err)
			continue
		}

		// 设置视频属性
		video.Visibility = biz.VisibilityType(visibility)
		video.Status = biz.VideoStatus(status)
		video.CoverURL = coverURL

		// 为每个视频获取变体
		// 注意：在大量视频的情况下，应该使用批处理或JOIN来优化
		variantsQuery := `SELECT resolution, codec, bitrate, url FROM video_variants WHERE video_id = ?`
		variantRows, err := r.data.db.QueryContext(ctx, variantsQuery, video.ID)
		if err == nil {
			for variantRows.Next() {
				var variant biz.VideoVariant
				if err := variantRows.Scan(&variant.Resolution, &variant.Codec, &variant.Bitrate, &variant.StorageURL); err != nil {
					r.log.Errorf("failed to scan variant: %v", err)
					continue
				}
				video.Variants = append(video.Variants, variant)
			}
			variantRows.Close()
		}

		videos = append(videos, &video)
	}

	return videos, nil
}

// Helper methods

// createProcessingTasks creates initial processing tasks for a video
func (r *videoRepo) createProcessingTasks(ctx context.Context, videoID int64) error {
	// 定义要创建的处理任务类型
	taskTypes := []string{
		TaskTypeTranscode,
		TaskTypeThumbnail,
		TaskTypeMetadataExtraction,
	}

	// 插入处理任务
	for _, taskType := range taskTypes {
		query := `INSERT INTO processing_tasks (video_id, type, status, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?)`

		now := time.Now()
		_, err := r.data.db.ExecContext(ctx, query,
			videoID,
			taskType,
			TaskStatusPending,
			now,
			now,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// 缓存视频元数据
func (r *videoRepo) cacheVideoMetadata(ctx context.Context, video *biz.Video) {
	videoJSON, err := json.Marshal(video)
	if err != nil {
		r.log.Errorf("failed to marshal video for caching: %v", err)
		return
	}

	// 使用{}确保相同videoID的所有键都映射到同一节点
	videoKey := fmt.Sprintf("video:{%d}", video.ID)

	// 设置缓存
	if err := r.data.rdb.Set(ctx, videoKey, videoJSON, 1*time.Hour).Err(); err != nil {
		r.log.Errorf("failed to cache video in Redis: %v", err)
	}
}
