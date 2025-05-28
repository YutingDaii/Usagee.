package service

import (
	"context"
	"net/http"
	"os"
	"regexp"
	"strconv"

	pb "user/api/user"

	"user/internal/biz"
	"user/internal/pkg/auth"
	"user/internal/pkg/generateID"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
)

func NewUserService(user *biz.UserUsecase, logger log.Logger) *UserService {
	return &UserService{
		uc:  user,
		log: log.NewHelper(logger),
	}
}

func (s *UserService) SendVerificationCode(ctx context.Context, req *pb.SendVerificationCodeReq) (*pb.SendVerificationCodeReply, error) {
	//正则检验手机号
	phoneRegex := regexp.MustCompile(`^1[3-9]\d{9}$`)
	if !phoneRegex.MatchString(req.Phone) {
		return &pb.SendVerificationCodeReply{}, errors.BadRequest("", "invalid phone number")
	}

	err := s.uc.SendVerificationCode(ctx, &biz.Code{Phone: req.Phone})
	if err != nil {
		log.Errorf("send verification code failed: %v", err)
		return &pb.SendVerificationCodeReply{}, errors.InternalServer(err.Error(), "send verification code failed")
	}
	return &pb.SendVerificationCodeReply{Success: true}, nil
}

func (s *UserService) VerifyCode(ctx context.Context, req *pb.VerifyCodeReq) (*pb.VerifyCodeReply, error) {
	//redis检验
	err := s.uc.VerifyCode(ctx, &biz.Code{Phone: req.Phone, VerifyCode: req.Code})
	if err != nil {
		log.Errorf("verify code failed: %v", err)
		return &pb.VerifyCodeReply{}, errors.New(http.StatusOK, err.Error(), "verify code failed")
	}

	// 环境变量获取
	workerIDStr := os.Getenv("WORKER_ID")
	if workerIDStr == "" {
		workerIDStr = "1" // Default value if not set
	}
	workerID, err := strconv.Atoi(workerIDStr)
	if err != nil {
		log.Errorf("invalid worker ID: %v", err)
		return &pb.VerifyCodeReply{}, errors.InternalServer(err.Error(), "invalid worker ID")
	}

	datacenterIDStr := os.Getenv("DATACENTER_ID")
	if datacenterIDStr == "" {
		datacenterIDStr = "1" // Default value if not set
	}
	datacenterID, err := strconv.Atoi(datacenterIDStr)
	if err != nil {
		log.Errorf("invalid datacenter ID: %v", err)
		return &pb.VerifyCodeReply{}, errors.InternalServer(err.Error(), "invalid datacenter ID")
	}
	//生成器
	generator, err := generateID.NewSnowflakeIDGenerator(int64(workerID), int64(datacenterID))
	if err != nil {
		log.Errorf("generate user ID failed: %v", err)
		return &pb.VerifyCodeReply{}, errors.InternalServer(err.Error(), "generate user ID failed")
	}
	//生成ID
	userID, err := generator.NextID()
	if err != nil {
		log.Errorf("generate user ID failed: %v", err)
		return &pb.VerifyCodeReply{}, errors.InternalServer(err.Error(), "generate user ID failed")
	}
	token, err := auth.GenerateToken(userID)
	if err != nil {
		log.Errorf("VerifyCode: generate token failed: %v", err)
		return &pb.VerifyCodeReply{}, errors.InternalServer(err.Error(), "generate token failed")
	}
	return &pb.VerifyCodeReply{Success: true, Token: token}, nil
}

func (s *UserService) Register(ctx context.Context, req *pb.RegisterUserReq) (*pb.RegisterUserReply, error) {
	u, err := s.uc.CreateUser(ctx, &biz.User{Phone: req.Phone, Password: req.Password})
	if err != nil {
		log.Errorf("create user failed: %v", err)
		return &pb.RegisterUserReply{}, errors.InternalServer(err.Error(), "create user failed")
	}
	token, err := auth.GenerateToken(u.ID)
	if err != nil {
		log.Errorf("Register: generate token failed: %v", err)
		return &pb.RegisterUserReply{}, errors.InternalServer(err.Error(), "generate token failed")
	}
	return &pb.RegisterUserReply{Id: u.ID, Token: token}, nil
}

func (s *UserService) Login(ctx context.Context, req *pb.LoginUserReq) (*pb.LoginUserReply, error) {
	u, err := s.uc.Login(ctx, &biz.User{Phone: req.Phone})
	if err != nil {
		log.Errorf("login user failed: %v", err)
		return &pb.LoginUserReply{}, errors.Errorf(http.StatusOK, err.Error(), "login user failed")
	}
	token, err := auth.GenerateToken(u.ID)
	if err != nil {
		log.Errorf("Login: generate token failed: %v", err)
		return &pb.LoginUserReply{}, errors.InternalServer(err.Error(), "generate token failed")
	}
	return &pb.LoginUserReply{Id: u.ID, Token: token}, nil
}

func (s *UserService) UpdateUser(ctx context.Context, req *pb.UpdateUserReq) (*pb.UpdateUserReply, error) {
	err := s.uc.UpdateUser(ctx, &biz.User{
		ID:       req.Id,
		Nickname: req.Nickname,
		Phone:    req.Phone,
		Password: req.Password,
		Avater:   req.Avatar,
		Bio:      req.Bio,
		Gender:   req.Gender,
		Birthday: req.Birthdate,
	})
	return &pb.UpdateUserReply{}, err
}

func (s *UserService) DeleteUser(ctx context.Context, req *pb.DeleteUserReq) (*pb.DeleteUserReply, error) {
	err := s.uc.DeleteUser(ctx, req.Id)

	return &pb.DeleteUserReply{}, err
}

func (s *UserService) GetUser(ctx context.Context, req *pb.GetUserReq) (*pb.GetUserReply, error) {
	u, err := s.uc.GetUserProfile(ctx, req.Id)
	if err != nil {
		return &pb.GetUserReply{}, err
	}
	return &pb.GetUserReply{Id: u.ID, Nickname: u.Nickname, Gender: u.Gender, Avater: u.Avater, Bio: u.Bio, Birthday: u.Birthday, FansCount: u.FansCount, FollowingCount: u.FollowingCount}, nil
}

func (s *UserService) ListFollowers(ctx context.Context, req *pb.ListFollowersReq) (*pb.ListFollowersReply, error) {
	users, err := s.uc.ListFollowers(ctx, req.Id)
	if err != nil {
		return &pb.ListFollowersReply{}, err
	}
	followers := []*pb.GetUserReply{}
	for _, u := range users {
		follower := pb.GetUserReply{
			Id:             u.ID,
			Nickname:       u.Nickname,
			Gender:         u.Gender,
			Avater:         u.Avater,
			Bio:            u.Bio,
			Birthday:       u.Birthday,
			FansCount:      u.FansCount,
			FollowingCount: u.FollowingCount,
		}
		followers = append(followers, &follower)
	}

	return &pb.ListFollowersReply{Followers: followers}, nil
}

func (s *UserService) ListFollowing(ctx context.Context, req *pb.ListFollowingReq) (*pb.ListFollowingReply, error) {
	users, err := s.uc.ListFollowings(ctx, req.Id)
	if err != nil {
		return &pb.ListFollowingReply{}, err
	}
	followings := []*pb.GetUserReply{}
	for _, u := range users {
		following := pb.GetUserReply{
			Id:             u.ID,
			Nickname:       u.Nickname,
			Gender:         u.Gender,
			Avater:         u.Avater,
			Bio:            u.Bio,
			Birthday:       u.Birthday,
			FansCount:      u.FansCount,
			FollowingCount: u.FollowingCount,
		}
		followings = append(followings, &following)
	}

	return &pb.ListFollowingReply{Following: followings}, nil
}

func (s *UserService) FollowUser(ctx context.Context, req *pb.FollowUserReq) (*pb.FollowUserReply, error) {
	err := s.uc.Follow(ctx, &biz.Follow{Follower_id: req.Id, Followed_id: req.FollowId})
	return &pb.FollowUserReply{}, err
}
