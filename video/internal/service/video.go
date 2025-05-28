package service

import (
	"context"
	"os"
	"strconv"
	pb "video/api/video"
	"video/internal/biz"
	"video/internal/pkg/generateID"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func NewVideoService(v *biz.VideoUsecase, logger log.Logger) *VideoService {
	return &VideoService{
		v:   v,
		log: log.NewHelper(logger),
	}
}

// CreateVideo handles the creation of a new video
func (s *VideoService) CreateVideo(ctx context.Context, req *pb.CreateVideoRequest) (*pb.VideoResponse, error) {
	if req.Video == nil {
		return nil, errors.BadRequest("VIDEO.MISSING_VIDEO", "video information is required")
	}

	// 从环境变量获取worker和datacenter ID
	workerIDStr := os.Getenv("WORKER_ID")
	if workerIDStr == "" {
		workerIDStr = "1" // 默认值
	}
	workerID, err := strconv.Atoi(workerIDStr)
	if err != nil {
		s.log.Errorf("invalid worker ID: %v", err)
		return nil, errors.InternalServer("VIDEO.INVALID_WORKER_ID", "invalid worker ID configuration")
	}

	datacenterIDStr := os.Getenv("DATACENTER_ID")
	if datacenterIDStr == "" {
		datacenterIDStr = "1" // 默认值
	}
	datacenterID, err := strconv.Atoi(datacenterIDStr)
	if err != nil {
		s.log.Errorf("invalid datacenter ID: %v", err)
		return nil, errors.InternalServer("VIDEO.INVALID_DATACENTER_ID", "invalid datacenter ID configuration")
	}

	// 创建雪花算法ID生成器
	generator, err := generateID.NewSnowflakeIDGenerator(int64(workerID), int64(datacenterID))
	if err != nil {
		s.log.Errorf("failed to create snowflake ID generator: %v", err)
		return nil, errors.InternalServer("VIDEO.ID_GENERATOR_FAILED", "failed to initialize ID generator")
	}

	// 生成视频ID
	videoID, err := generator.NextID()
	if err != nil {
		s.log.Errorf("failed to generate video ID: %v", err)
		return nil, errors.InternalServer("VIDEO.ID_GENERATION_FAILED", "failed to generate video ID")
	}

	video := &biz.Video{
		ID:          videoID, // 使用生成的雪花ID
		UserID:      req.Video.UserId,
		Title:       req.Video.Title,
		Description: req.Video.Description,
		Duration:    int(req.Video.Duration),
		Visibility:  biz.VisibilityType(req.Video.Visibility.String()),
	}

	result, err := s.v.CreateVideo(ctx, video)
	if err != nil {
		s.log.Errorf("Failed to create video: %v", err)
		return nil, errors.InternalServer("VIDEO.CREATE_FAILED", "failed to create video")
	}

	return &pb.VideoResponse{
		Video: convertToPbVideo(result),
	}, nil
}

// TranscodeVideo handles video transcoding
func (s *VideoService) TranscodeVideo(ctx context.Context, req *pb.TranscodeVideoRequest) (*pb.TranscodeVideoResponse, error) {
	video, err := s.v.GetVideo(ctx, req.VideoId)
	if err != nil {
		s.log.Errorf("Failed to get video for transcoding: %v", err)
		return nil, errors.NotFound("VIDEO.NOT_FOUND", "video not found")
	}

	variants, err := s.v.TranscodeVideo(ctx, video)
	if err != nil {
		s.log.Errorf("Failed to transcode video: %v", err)
		return nil, errors.InternalServer("VIDEO.TRANSCODE_FAILED", "failed to transcode video")
	}

	pbVariants := make([]*pb.VideoVariant, 0, len(variants))
	for _, v := range variants {
		pbVariants = append(pbVariants, &pb.VideoVariant{
			Id:         0, // ID will be assigned in the data layer
			VideoId:    video.ID,
			Resolution: v.Resolution,
			Url:        v.StorageURL,
		})
	}

	return &pb.TranscodeVideoResponse{
		Variants: pbVariants,
	}, nil
}

// GenerateThumbnail generates a thumbnail for a video
func (s *VideoService) GenerateThumbnail(ctx context.Context, req *pb.GenerateThumbnailRequest) (*pb.GenerateThumbnailResponse, error) {
	video, err := s.v.GetVideo(ctx, req.VideoId)
	if err != nil {
		s.log.Errorf("Failed to get video for thumbnail generation: %v", err)
		return nil, errors.NotFound("VIDEO.NOT_FOUND", "video not found")
	}

	thumbnailURL, err := s.v.GenerateThumbnail(ctx, video)
	if err != nil {
		s.log.Errorf("Failed to generate thumbnail: %v", err)
		return nil, errors.InternalServer("VIDEO.THUMBNAIL_FAILED", "failed to generate thumbnail")
	}

	return &pb.GenerateThumbnailResponse{
		ThumbnailUrl: thumbnailURL,
	}, nil
}

// GetVideo retrieves a video by ID
func (s *VideoService) GetVideo(ctx context.Context, req *pb.GetVideoRequest) (*pb.VideoResponse, error) {
	video, err := s.v.GetVideo(ctx, req.Id)
	if err != nil {
		s.log.Errorf("Failed to get video: %v", err)
		return nil, errors.NotFound("VIDEO.NOT_FOUND", "video not found")
	}

	return &pb.VideoResponse{
		Video: convertToPbVideo(video),
	}, nil
}

// DeleteVideo handles video deletion
func (s *VideoService) DeleteVideo(ctx context.Context, req *pb.DeleteVideoRequest) (*pb.DeleteVideoResponse, error) {
	err := s.v.DeleteVideo(ctx, req.Id)
	if err != nil {
		s.log.Errorf("Failed to delete video: %v", err)
		return nil, errors.InternalServer("VIDEO.DELETE_FAILED", "failed to delete video")
	}

	return &pb.DeleteVideoResponse{}, nil
}

// GetVideoByUserID retrieves videos by user ID
func (s *VideoService) GetVideoByUserID(ctx context.Context, req *pb.GetVideoByUserIDRequest) (*pb.VideoListResponse, error) {
	videos, err := s.v.GetVideoByUserID(ctx, req.UserId)
	if err != nil {
		s.log.Errorf("Failed to get videos by user ID: %v", err)
		return nil, errors.InternalServer("VIDEO.LIST_FAILED", "failed to list videos")
	}

	pbVideos := make([]*pb.Video, 0, len(videos))
	for _, v := range videos {
		pbVideos = append(pbVideos, convertToPbVideo(v))
	}

	return &pb.VideoListResponse{
		Videos: pbVideos,
	}, nil
}

// Helper function to convert biz.Video to pb.Video
func convertToPbVideo(v *biz.Video) *pb.Video {
	pbVideo := &pb.Video{
		Id:          v.ID,
		UserId:      v.UserID,
		Title:       v.Title,
		Description: v.Description,
		Duration:    int32(v.Duration),
		Url:         "", // Will be set if variants exist
		Resolution:  "", // Will be set if variants exist
		CreatedAt:   timestamppb.New(v.CreatedAt),
		UpdatedAt:   timestamppb.New(v.UpdatedAt),
	}

	// Set visibility enum
	switch v.Visibility {
	case biz.Public:
		pbVideo.Visibility = pb.Video_PUBLIC
	case biz.Private:
		pbVideo.Visibility = pb.Video_PRIVATE
	default:
		pbVideo.Visibility = pb.Video_PUBLIC
	}

	// Set status enum
	switch v.Status {
	case biz.DraftStatus:
		pbVideo.Status = pb.Video_DRAFT
	case biz.UploadingStatus:
		pbVideo.Status = pb.Video_UPLOADING
	case biz.ProcessingStatus:
		pbVideo.Status = pb.Video_PROCESSING
	case biz.ReadyStatus:
		pbVideo.Status = pb.Video_READY
	case biz.PublishedStatus:
		pbVideo.Status = pb.Video_PUBLISHED
	case biz.UnpublishedStatus:
		pbVideo.Status = pb.Video_UNPUBLISHED
	default:
		pbVideo.Status = pb.Video_DRAFT
	}

	// Use the primary variant URL and resolution if available
	if len(v.Variants) > 0 {
		pbVideo.Url = v.Variants[0].StorageURL
		pbVideo.Resolution = v.Variants[0].Resolution
	}

	return pbVideo
}
