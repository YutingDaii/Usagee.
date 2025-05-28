package service

import (
	"context"
	"interaction/internal/biz" // Standard import path
	pb "interaction/api/interaction" // Standard import path for generated protobuf

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// InteractionService is a gRPC service for interactions.
type InteractionService struct {
	pb.UnimplementedInteractionServiceServer // Embed for forward compatibility

	uc  *biz.InteractionUsecase
	log *log.Helper
}

// NewInteractionService creates a new InteractionService.
func NewInteractionService(uc *biz.InteractionUsecase, logger log.Logger) *InteractionService {
	return &InteractionService{
		uc:  uc,
		log: log.NewHelper(log.With(logger, "module", "service/interaction")),
	}
}

// LikeVideo handles liking a video.
func (s *InteractionService) LikeVideo(ctx context.Context, req *pb.LikeVideoRequest) (*pb.LikeVideoReply, error) {
	s.log.WithContext(ctx).Infof("LikeVideo called with UserID: %d, VideoID: %d", req.UserId, req.VideoId)
	err := s.uc.LikeVideo(ctx, req.UserId, req.VideoId)
	if err != nil {
		s.log.WithContext(ctx).Errorf("LikeVideo failed: %v", err)
		return nil, err // Consider mapping to gRPC error codes
	}
	return &pb.LikeVideoReply{Success: true}, nil
}

// UnlikeVideo handles unliking a video.
func (s *InteractionService) UnlikeVideo(ctx context.Context, req *pb.UnlikeVideoRequest) (*pb.UnlikeVideoReply, error) {
	s.log.WithContext(ctx).Infof("UnlikeVideo called with UserID: %d, VideoID: %d", req.UserId, req.VideoId)
	err := s.uc.UnlikeVideo(ctx, req.UserId, req.VideoId)
	if err != nil {
		s.log.WithContext(ctx).Errorf("UnlikeVideo failed: %v", err)
		return nil, err
	}
	return &pb.UnlikeVideoReply{Success: true}, nil
}

// GetVideoLikeCount handles getting the like count for a video.
func (s *InteractionService) GetVideoLikeCount(ctx context.Context, req *pb.GetVideoLikeCountRequest) (*pb.GetVideoLikeCountReply, error) {
	s.log.WithContext(ctx).Infof("GetVideoLikeCount called with VideoID: %d", req.VideoId)
	count, err := s.uc.GetVideoLikeCount(ctx, req.VideoId)
	if err != nil {
		s.log.WithContext(ctx).Errorf("GetVideoLikeCount failed: %v", err)
		return nil, err
	}
	return &pb.GetVideoLikeCountReply{LikeCount: count}, nil
}

// PostComment handles posting a comment on a video or replying to a comment.
func (s *InteractionService) PostComment(ctx context.Context, req *pb.PostCommentRequest) (*pb.PostCommentReply, error) {
	s.log.WithContext(ctx).Infof("PostComment called with VideoID: %d, UserID: %d, ParentID: %d", req.VideoId, req.UserId, req.ParentId)
	
	// The biz.InteractionUsecase.PostComment expects CommentID to be generated beforehand if it's part of the path.
	// This is a common challenge. Options:
	// 1. Generate ID here (e.g., Snowflake) and pass to usecase.
	// 2. Usecase generates ID.
	// 3. Repo generates ID and path (might require two steps or DB functions).
	// For now, assuming usecase or repo handles ID generation and path construction.
	// The current biz.PostComment implies it might need a pre-generated ID for path.
	// Let's assume for now that the biz layer handles this complexity.
	// If CommentID is a string (UUID), it should be generated here or in biz.
	// If CommentID is an int64 auto-increment, path logic in repo needs care.

	// The design doc has CommentItem.CommentID as int64.
	// The table `comments` has `id VARCHAR(36)`. This is a mismatch.
	// Assuming `CommentID` in `CommentItem` (biz & proto) should map to `id` in `comments` table.
	// If `id` is `VARCHAR(36)`, it's likely a UUID.
	// Let's assume the biz layer's `PostComment` will return a `CommentItem` with the `CommentID` (and `Path`) populated.

	bizComment, err := s.uc.PostComment(ctx, req.VideoId, req.UserId, req.Content, req.ParentId)
	if err != nil {
		s.log.WithContext(ctx).Errorf("PostComment failed: %v", err)
		return nil, err
	}

	// Convert biz.CommentItem to pb.CommentItem
	pbComment := &pb.CommentItem{
		CommentId: bizComment.CommentID,
		VideoId:   bizComment.VideoID,
		UserId:    bizComment.UserID,
		Content:   bizComment.Content,
		ParentId:  bizComment.ParentID,
		CreatedAt: timestamppb.New(bizComment.CreatedAt),
		Path:      bizComment.Path,
		// Replies are not typically returned on PostComment, but could be if needed.
	}

	return &pb.PostCommentReply{Comment: pbComment}, nil
}

// ListComments handles listing comments for a video.
func (s *InteractionService) ListComments(ctx context.Context, req *pb.ListCommentsRequest) (*pb.ListCommentsReply, error) {
	s.log.WithContext(ctx).Infof("ListComments called for VideoID: %d, Page: %d, Size: %d, RootCommentID: %d", req.VideoId, req.PageNum, req.PageSize, req.RootCommentId)
	
	bizComments, totalComments, err := s.uc.ListComments(ctx, req.VideoId, req.PageNum, req.PageSize, req.RootCommentId)
	if err != nil {
		s.log.WithContext(ctx).Errorf("ListComments failed: %v", err)
		return nil, err
	}

	pbComments := make([]*pb.CommentItem, len(bizComments))
	for i, bc := range bizComments {
		pbComments[i] = &pb.CommentItem{
			CommentId: bc.CommentID,
			VideoId:   bc.VideoID,
			UserId:    bc.UserID,
			Content:   bc.Content,
			ParentId:  bc.ParentID,
			CreatedAt: timestamppb.New(bc.CreatedAt),
			Path:      bc.Path,
			Replies:   convertBizRepliesToPB(bc.Replies), // Helper to convert nested replies
		}
	}
	
	// Calculate total pages
	var totalPages int32
	if totalComments > 0 && req.PageSize > 0 {
		totalPages = int32((totalComments + int64(req.PageSize) - 1) / int64(req.PageSize))
	}


	return &pb.ListCommentsReply{
		Comments:     pbComments,
		TotalPages:   totalPages,
		TotalComments: totalComments,
	}, nil
}

// Helper function to convert biz.CommentItem replies to pb.CommentItem replies
func convertBizRepliesToPB(bizReplies []*biz.CommentItem) []*pb.CommentItem {
	if bizReplies == nil {
		return nil
	}
	pbReplies := make([]*pb.CommentItem, len(bizReplies))
	for i, br := range bizReplies {
		pbReplies[i] = &pb.CommentItem{
			CommentId: br.CommentID,
			VideoId:   br.VideoID,
			UserId:    br.UserID,
			Content:   br.Content,
			ParentId:  br.ParentID,
			CreatedAt: timestamppb.New(br.CreatedAt),
			Path:      br.Path,
			Replies:   convertBizRepliesToPB(br.Replies), // Recursive call for nested replies
		}
	}
	return pbReplies
}


// FavoriteVideo handles adding a video to favorites.
func (s *InteractionService) FavoriteVideo(ctx context.Context, req *pb.FavoriteVideoRequest) (*pb.FavoriteVideoReply, error) {
	s.log.WithContext(ctx).Infof("FavoriteVideo called with UserID: %d, VideoID: %d", req.UserId, req.VideoId)
	err := s.uc.FavoriteVideo(ctx, req.UserId, req.VideoId)
	if err != nil {
		s.log.WithContext(ctx).Errorf("FavoriteVideo failed: %v", err)
		return nil, err
	}
	return &pb.FavoriteVideoReply{Success: true}, nil
}

// UnfavoriteVideo handles removing a video from favorites.
func (s *InteractionService) UnfavoriteVideo(ctx context.Context, req *pb.UnfavoriteVideoRequest) (*pb.UnfavoriteVideoReply, error) {
	s.log.WithContext(ctx).Infof("UnfavoriteVideo called with UserID: %d, VideoID: %d", req.UserId, req.VideoId)
	err := s.uc.UnfavoriteVideo(ctx, req.UserId, req.VideoId)
	if err != nil {
		s.log.WithContext(ctx).Errorf("UnfavoriteVideo failed: %v", err)
		return nil, err
	}
	return &pb.UnfavoriteVideoReply{Success: true}, nil
}

// ListFavorites handles listing a user's favorite videos.
func (s *InteractionService) ListFavorites(ctx context.Context, req *pb.ListFavoritesRequest) (*pb.ListFavoritesReply, error) {
	s.log.WithContext(ctx).Infof("ListFavorites called for UserID: %d, Page: %d, Size: %d", req.UserId, req.PageNum, req.PageSize)
	bizFavorites, totalFavorites, err := s.uc.ListFavorites(ctx, req.UserId, req.PageNum, req.PageSize)
	if err != nil {
		s.log.WithContext(ctx).Errorf("ListFavorites failed: %v", err)
		return nil, err
	}

	pbFavorites := make([]*pb.FavoriteItem, len(bizFavorites))
	for i, bf := range bizFavorites {
		pbFavorites[i] = &pb.FavoriteItem{
			UserId:    bf.UserID,
			VideoId:   bf.VideoID,
			FavoritedAt: timestamppb.New(bf.FavoritedAt),
		}
	}

	var totalPages int32
	if totalFavorites > 0 && req.PageSize > 0 {
		totalPages = int32((totalFavorites + int64(req.PageSize) - 1) / int64(req.PageSize))
	}

	return &pb.ListFavoritesReply{
		Favorites:     pbFavorites,
		TotalPages:   totalPages,
		TotalFavorites: totalFavorites,
	}, nil
}
