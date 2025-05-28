package biz

import (
	"context"
	"errors" // Added for ErrNotFound
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

// Predefined errors
var (
	ErrNotFound = errors.New("resource not found")
	// Add other common business errors here
)

// LikeItem is a domain model for like records.
type LikeItem struct {
	UserID  int64
	VideoID int64
	LikedAt time.Time
}

// CommentItem is a domain model for comments and replies.
type CommentItem struct {
	CommentID int64
	VideoID   int64
	UserID    int64
	Content   string
	ParentID  int64 // 0 for root comment
	CreatedAt time.Time
	Path      string          // Materialized path, e.g., "root/123/456"
	Replies   []*CommentItem // Nested replies
}

// FavoriteItem is a domain model for favorite records.
type FavoriteItem struct {
	UserID      int64
	VideoID     int64
	FavoritedAt time.Time
}

// InteractionRepo defines the storage interface for interaction data.
// This interface will be implemented by the data layer.
type InteractionRepo interface {
	// Like operations
	CreateLike(ctx context.Context, item *LikeItem) error
	DeleteLike(ctx context.Context, userID, videoID int64) error
	GetVideoLikeCount(ctx context.Context, videoID int64) (int64, error)
	IsLiked(ctx context.Context, userID, videoID int64) (bool, error) // Helper to check if user liked a video

	// Comment operations
	CreateComment(ctx context.Context, item *CommentItem) (*CommentItem, error)
	ListCommentsByVideoID(ctx context.Context, videoID int64, pageNum, pageSize int32) ([]*CommentItem, int64, error)
	ListRepliesByParentID(ctx context.Context, parentID int64, pageNum, pageSize int32) ([]*CommentItem, int64, error)
	GetCommentByID(ctx context.Context, commentID int64) (*CommentItem, error) // For fetching individual comments, potentially for replies

	// Favorite operations
	CreateFavorite(ctx context.Context, item *FavoriteItem) error
	DeleteFavorite(ctx context.Context, userID, videoID int64) error
	ListFavoritesByUserID(ctx context.Context, userID int64, pageNum, pageSize int32) ([]*FavoriteItem, int64, error)
	IsFavorited(ctx context.Context, userID, videoID int64) (bool, error) // Helper to check if user favorited a video
}

// InteractionUsecase is the use case for interaction operations.
// It orchestrates the business logic using the InteractionRepo.
type InteractionUsecase struct {
	repo InteractionRepo
	log  *log.Helper
	// Kafka producer or event bus can be added here if needed for domain events
}

// NewInteractionUsecase creates a new InteractionUsecase.
func NewInteractionUsecase(repo InteractionRepo, logger log.Logger) *InteractionUsecase {
	return &InteractionUsecase{
		repo: repo,
		log:  log.NewHelper(log.With(logger, "module", "usecase/interaction")),
		// Initialize Kafka producer here if applicable
	}
}

// LikeVideo handles the logic for liking a video.
func (uc *InteractionUsecase) LikeVideo(ctx context.Context, userID, videoID int64) error {
	// Check if already liked to prevent duplicate entries if necessary,
	// or rely on DB constraints. For simplicity, we'll assume DB handles this.
	item := &LikeItem{
		UserID:  userID,
		VideoID: videoID,
		LikedAt: time.Now(),
	}
	err := uc.repo.CreateLike(ctx, item)
	if err != nil {
		uc.log.Errorf("Failed to like video: %v", err)
		return err
	}
	// TODO: Publish VideoLikedEvent to Kafka
	uc.log.Infof("User %d liked video %d", userID, videoID)
	return nil
}

// UnlikeVideo handles the logic for unliking a video.
func (uc *InteractionUsecase) UnlikeVideo(ctx context.Context, userID, videoID int64) error {
	err := uc.repo.DeleteLike(ctx, userID, videoID)
	if err != nil {
		uc.log.Errorf("Failed to unlike video: %v", err)
		return err
	}
	// TODO: Publish VideoUnlikedEvent or use VideoLikedEvent with a flag
	uc.log.Infof("User %d unliked video %d", userID, videoID)
	return nil
}

// GetVideoLikeCount retrieves the like count for a video.
func (uc *InteractionUsecase) GetVideoLikeCount(ctx context.Context, videoID int64) (int64, error) {
	count, err := uc.repo.GetVideoLikeCount(ctx, videoID)
	if err != nil {
		uc.log.Errorf("Failed to get video like count for video %d: %v", videoID, err)
		return 0, err
	}
	return count, nil
}

// PostComment handles posting a new comment or a reply.
func (uc *InteractionUsecase) PostComment(ctx context.Context, videoID, userID int64, content string, parentID int64) (*CommentItem, error) {
	comment := &CommentItem{
		VideoID:   videoID,
		UserID:    userID,
		Content:   content,
		ParentID:  parentID,
		CreatedAt: time.Now(),
	}

	if parentID != 0 {
		// This is a reply, construct the path based on the parent.
		parentComment, err := uc.repo.GetCommentByID(ctx, parentID)
		if err != nil {
			uc.log.Errorf("Failed to get parent comment %d for reply: %v", parentID, err)
			return nil, err // Or handle as a root comment if parent not found
		}
		// Basic path construction, can be more sophisticated.
		// Assuming parentComment.Path is valid.
		// comment.Path = parentComment.Path + "/" + "generated_comment_id" // ID will be set by repo
	} else {
		// This is a root comment.
		// comment.Path = "root" // Or "root/generated_comment_id"
	}
	// The actual path generation might be better handled in the repo or after ID generation.
	// For now, we'll let the repo handle path creation if it needs the ID.

	createdComment, err := uc.repo.CreateComment(ctx, comment)
	if err != nil {
		uc.log.Errorf("Failed to post comment: %v", err)
		return nil, err
	}
	// TODO: Publish CommentPostedEvent to Kafka
	uc.log.Infof("User %d posted comment on video %d (parent: %d)", userID, videoID, parentID)
	return createdComment, nil
}

// ListComments retrieves comments for a video, potentially with pagination and replies.
func (uc *InteractionUsecase) ListComments(ctx context.Context, videoID int64, pageNum, pageSize int32, rootCommentID int64) ([]*CommentItem, int64, error) {
	var comments []*CommentItem
	var totalComments int64
	var err error

	if rootCommentID != 0 {
		// Listing replies for a specific root comment
		comments, totalComments, err = uc.repo.ListRepliesByParentID(ctx, rootCommentID, pageNum, pageSize)
	} else {
		// Listing root comments for a video
		comments, totalComments, err = uc.repo.ListCommentsByVideoID(ctx, videoID, pageNum, pageSize)
	}

	if err != nil {
		uc.log.Errorf("Failed to list comments for video %d (root: %d): %v", videoID, rootCommentID, err)
		return nil, 0, err
	}
	
	// Optionally, fetch nested replies for each comment if not handled by the repo.
	// This can lead to N+1 queries if not careful.
	// For simplicity, assuming ListCommentsByVideoID and ListRepliesByParentID can populate direct replies
	// or a separate call is made by the service layer if deep nesting is needed.

	return comments, totalComments, nil
}

// FavoriteVideo handles adding a video to a user's favorites.
func (uc *InteractionUsecase) FavoriteVideo(ctx context.Context, userID, videoID int64) error {
	item := &FavoriteItem{
		UserID:      userID,
		VideoID:     videoID,
		FavoritedAt: time.Now(),
	}
	err := uc.repo.CreateFavorite(ctx, item)
	if err != nil {
		uc.log.Errorf("Failed to favorite video: %v", err)
		return err
	}
	// TODO: Publish VideoFavoritedEvent to Kafka
	uc.log.Infof("User %d favorited video %d", userID, videoID)
	return nil
}

// UnfavoriteVideo handles removing a video from a user's favorites.
func (uc *InteractionUsecase) UnfavoriteVideo(ctx context.Context, userID, videoID int64) error {
	err := uc.repo.DeleteFavorite(ctx, userID, videoID)
	if err != nil {
		uc.log.Errorf("Failed to unfavorite video: %v", err)
		return err
	}
	// TODO: Publish VideoUnfavoritedEvent or use VideoFavoritedEvent with a flag
	uc.log.Infof("User %d unfavorited video %d", userID, videoID)
	return nil
}

// ListFavorites retrieves a user's list of favorite videos.
func (uc *InteractionUsecase) ListFavorites(ctx context.Context, userID int64, pageNum, pageSize int32) ([]*FavoriteItem, int64, error) {
	favorites, totalFavorites, err := uc.repo.ListFavoritesByUserID(ctx, userID, pageNum, pageSize)
	if err != nil {
		uc.log.Errorf("Failed to list favorites for user %d: %v", userID, err)
		return nil, 0, err
	}
	return favorites, totalFavorites, nil
}
