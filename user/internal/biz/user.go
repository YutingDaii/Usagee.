package biz

import (
	"context"
	"math/rand"
	"strconv"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID             int64
	Nickname       string
	Phone          string
	Password       string
	Avater         string
	Bio            string
	Gender         string
	Birthday       string
	FansCount      int64
	FollowingCount int64
}

type Follow struct {
	Follower_id int64
	Followed_id int64
}

type Code struct {
	Phone      string
	VerifyCode string
}

// User DB interface
type UserRepo interface {
	CreateUser(ctx context.Context, u *User) (*User, error)
	Login(ctx context.Context, u *User) (*User, error)
	UpdateUser(ctx context.Context, u *User) error
	GetUserProfile(ctx context.Context, id int64) (*User, error)
	DeleteUser(ctx context.Context, id int64) error
	Follow(ctx context.Context, follow *Follow) error
	ListFollowers(ctx context.Context, id int64) ([]*User, error)
	ListFollowings(ctx context.Context, id int64) ([]*User, error)

	//redis
	SendVerificationCode(ctx context.Context, code *Code) error
	VerifyCode(ctx context.Context, code *Code) error

	//Kafka
	SendFollowEvent(ctx context.Context, follow *Follow) error
}

type UserUsecase struct {
	repo UserRepo
	log  *log.Helper
}

func NewUserUsecase(repo UserRepo, logger log.Logger) *UserUsecase {
	return &UserUsecase{
		repo: repo,
		log:  log.NewHelper(logger),
	}
}

func (uc *UserUsecase) SendVerificationCode(ctx context.Context, code *Code) error {
	// Generate a random 6-digit verification code
	rand.Seed(time.Now().UnixNano())
	verifyCode := rand.Intn(900000) + 100000 // ensures a 6-digit number
	//TODO: send the code to the phone number

	code.VerifyCode = strconv.Itoa(verifyCode)

	// Store the code in the redis
	return uc.repo.SendVerificationCode(ctx, code)
}

func (uc *UserUsecase) Login(ctx context.Context, user *User) (u *User, err error) {
	return uc.repo.Login(ctx, user)
}

func (uc *UserUsecase) VerifyCode(ctx context.Context, code *Code) error {
	return uc.repo.VerifyCode(ctx, code)
}

func (uc *UserUsecase) CreateUser(ctx context.Context, user *User) (u *User, err error) {
	user.Nickname = randomString(rand.Intn(10) + 5)
	//encrypt
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user.Password = string(hashedPassword)
	return uc.repo.CreateUser(ctx, user)
}

func (uc *UserUsecase) UpdateUser(ctx context.Context, user *User) (err error) {
	return uc.repo.UpdateUser(ctx, user)
}

func (uc *UserUsecase) GetUserProfile(ctx context.Context, id int64) (*User, error) {
	return uc.repo.GetUserProfile(ctx, id)
}

func (uc *UserUsecase) DeleteUser(ctx context.Context, id int64) error {
	return uc.repo.DeleteUser(ctx, id)
}

func (uc *UserUsecase) Follow(ctx context.Context, follow *Follow) error {
	err := uc.repo.Follow(ctx, follow)
	if err != nil {
		return err
	}
	err = uc.repo.SendFollowEvent(ctx, follow)
	if err != nil {
		uc.log.Errorf("send follow event failed: %v", err)
	}
	return err
}

func (uc *UserUsecase) ListFollowers(ctx context.Context, id int64) ([]*User, error) {
	return uc.repo.ListFollowers(ctx, id)
}

func (uc *UserUsecase) ListFollowings(ctx context.Context, id int64) ([]*User, error) {
	return uc.repo.ListFollowings(ctx, id)

}

// 生成随机字符串
const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomString(length int) string {
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}
