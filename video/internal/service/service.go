package service

import (
	"video/internal/biz"
	pb "video/api/video"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
)

// ProviderSet is service providers.
var ProviderSet = wire.NewSet(NewVideoService)
type VideoService struct {
	pb.UnimplementedVideoServiceServer
	v *biz.VideoUsecase
	log *log.Helper
}