package biz

import (
	"context"
	"errors"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

// internal/biz/video.go

// 视频可见性类型
type VisibilityType string

const (
	Public  VisibilityType = "PUBLIC"  // 公开可见
	Private VisibilityType = "PRIVATE" // 私密可见
)

// 视频状态机（严格约束生命周期）
type VideoStatus string

const (
	//视频创建时的默认状态
	DraftStatus VideoStatus = "DRAFT" // 初始草稿
	//用户提交了视频文件
	UploadingStatus VideoStatus = "UPLOADING" // 上传中
	//开始处理转码等任务
	ProcessingStatus VideoStatus = "PROCESSING" // 处理中
	//视频任务处理完
	ReadyStatus VideoStatus = "READY" // 处理完成待发布
	//视频处理失败时的状态
	FailedStatus VideoStatus = "FAILED" // 处理失败
	//视频发布后的状态
	PublishedStatus   VideoStatus = "PUBLISHED"   // 已发布
	UnpublishedStatus VideoStatus = "UNPUBLISHED" // 已下架
	//视频删除后的状态
	DeletedStatus VideoStatus = "DELETED" // 已删除（软删除）
)

// 视频变体值对象（不可变）
type VideoVariant struct {
	Resolution string // 分辨率 720p/1080p
	Codec      string // 编码格式 H264/HEVC
	Bitrate    int    // 码率 (kbps)
	StorageURL string // 存储路径
}

// Video聚合根
type Video struct {
	ID          int64          // 视频唯一ID（Snowflake生成）
	UserID      int64          // 上传用户ID
	Title       string         // 标题（业务规则：长度校验）
	Description string         // 描述
	Duration    int            // 时长（秒）
	Visibility  VisibilityType // 可见性
	Status      VideoStatus    // 当前状态
	Variants    []VideoVariant // 转码变体列表
	CoverURL    string         // 封面图URL
	CreatedAt   time.Time      // 创建时间
	UpdatedAt   time.Time      // 最后更新时间
}

// 状态转换规则矩阵
var stateTransitionRules = map[VideoStatus][]VideoStatus{
	DraftStatus:       {UploadingStatus, DeletedStatus}, // 草稿状态可以转换为上传中或删除
	UploadingStatus:   {ProcessingStatus},               //上传中可以转换为处理中
	ProcessingStatus:  {ReadyStatus, FailedStatus},      // 处理中可以转换为处理完成或失败
	ReadyStatus:       {PublishedStatus},                // 处理完成可以转换为已发布
	PublishedStatus:   {UnpublishedStatus},              // // 已发布可以转换为已下架
	UnpublishedStatus: {PublishedStatus, DeletedStatus}, //已下架可以转换为已发布或删除
}

type VideoRepo interface {
	//元数据
	CreateVideo(ctx context.Context, video *Video) (*Video, error)
	//转码
	TranscodeVideo(ctx context.Context, video *Video) ([]VideoVariant, error)
	//缩略图
	GenerateThumbnail(ctx context.Context, video *Video) (string, error)
	GetVideo(ctx context.Context, id int64) (*Video, error)
	DeleteVideo(ctx context.Context, id int64) error
	GetVideoByUserID(ctx context.Context, userID int64) ([]*Video, error)
}

type VideoUsecase struct {
	repo VideoRepo
	log  *log.Helper
}

func NewVideoUsecase(repo VideoRepo, logger log.Logger) *VideoUsecase {
	return &VideoUsecase{
		repo: repo,
		log:  log.NewHelper(logger),
	}
}

func (v *VideoUsecase) CreateVideo(ctx context.Context, video *Video) (*Video, error) {
	if video == nil || video.ID == 0 || video.UserID == 0 || video.Title == "" || video.Duration == 0 {
		v.log.Error("CreateVideo: invalid video data")
		return nil, errors.New("video cannot be nil")
	}

	video.Status = DraftStatus // 设置初始状态为草稿
	video.CreatedAt = time.Now()
	video.UpdatedAt = time.Now()
	return v.repo.CreateVideo(ctx, video)
}

func (v *VideoUsecase) GetVideo(ctx context.Context, id int64) (*Video, error) {
	video, err := v.repo.GetVideo(ctx, id)
	if err != nil {
		v.log.Error("GetVideo: error fetching video", err)
		return nil, err
	}
	return video, nil
}

func (v *VideoUsecase) DeleteVideo(ctx context.Context, id int64) error {
	video, err := v.repo.GetVideo(ctx, id)
	if err != nil {
		v.log.Error("DeleteVideo: error fetching video", err)
		return err
	}
	if video.Status == DeletedStatus {
		return errors.New("video already deleted")
	}
	video.Status = DeletedStatus // 设置状态为已删除
	video.UpdatedAt = time.Now()
	return v.repo.DeleteVideo(ctx, id)
}

func (v *VideoUsecase) GetVideoByUserID(ctx context.Context, userID int64) ([]*Video, error) {
	videoList, err := v.repo.GetVideoByUserID(ctx, userID)
	if err != nil {
		v.log.Error("GetVideoByUserID: error fetching videos", err)
		return nil, err
	}
	return videoList, nil
}

// TranscodeVideo handles video transcoding process
func (v *VideoUsecase) TranscodeVideo(ctx context.Context, video *Video) ([]VideoVariant, error) {
	if video == nil || video.ID == 0 {
		return nil, errors.New("invalid video data for transcoding")
	}

	// Verify the video exists and can be transcoded
	if video.Status != DraftStatus && video.Status != UploadingStatus {
		return nil, errors.New("video is not in a valid state for transcoding")
	}

	// Change state to processing
	if !canTransition(video.Status, ProcessingStatus) {
		return nil, errors.New("invalid state transition")
	}

	video.Status = ProcessingStatus
	video.UpdatedAt = time.Now()

	// Call the repository to handle the transcoding process
	variants, err := v.repo.TranscodeVideo(ctx, video)
	if err != nil {
		v.log.Error("TranscodeVideo: error during transcoding", err)
		return nil, err
	}

	// Return the generated variants
	return variants, nil
}

// GenerateThumbnail creates a thumbnail for the video
func (v *VideoUsecase) GenerateThumbnail(ctx context.Context, video *Video) (string, error) {
	if video == nil || video.ID == 0 {
		return "", errors.New("invalid video data for thumbnail generation")
	}

	// Verify the video is in a valid state
	if video.Status == DeletedStatus {
		return "", errors.New("cannot generate thumbnail for deleted video")
	}

	// Call the repository to generate the thumbnail
	thumbnailURL, err := v.repo.GenerateThumbnail(ctx, video)
	if err != nil {
		v.log.Error("GenerateThumbnail: error generating thumbnail", err)
		return "", err
	}

	return thumbnailURL, nil
}

// PublishVideo changes video status to published
func (v *VideoUsecase) PublishVideo(ctx context.Context, id int64) error {
	video, err := v.repo.GetVideo(ctx, id)
	if err != nil {
		v.log.Error("PublishVideo: error fetching video", err)
		return err
	}

	// Verify the video can be published
	if !canTransition(video.Status, PublishedStatus) {
		return errors.New("video cannot be published from its current state")
	}

	video.Status = PublishedStatus
	video.UpdatedAt = time.Now()

	// Save the updated status
	_, err = v.repo.CreateVideo(ctx, video) // Reuse CreateVideo for updates
	return err
}

// UnpublishVideo changes video status to unpublished
func (v *VideoUsecase) UnpublishVideo(ctx context.Context, id int64) error {
	video, err := v.repo.GetVideo(ctx, id)
	if err != nil {
		v.log.Error("UnpublishVideo: error fetching video", err)
		return err
	}

	// Verify the video can be unpublished
	if !canTransition(video.Status, UnpublishedStatus) {
		return errors.New("video cannot be unpublished from its current state")
	}

	video.Status = UnpublishedStatus
	video.UpdatedAt = time.Now()

	// Save the updated status
	_, err = v.repo.CreateVideo(ctx, video) // Reuse CreateVideo for updates
	return err
}

// 判断是否可以状态转换
func canTransition(from VideoStatus, to VideoStatus) bool {
	if allowedStates, exists := stateTransitionRules[from]; exists {
		return contains(allowedStates, to)
	}
	return false
}

func contains(slice []VideoStatus, target VideoStatus) bool {
	for _, v := range slice {
		if v == target {
			return true
		}
	}
	return false
}
