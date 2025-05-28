package data

import (
	"context"
	"errors"
	"strconv"

	"user/internal/biz"
	models "user/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
)

type userRepo struct {
	data *Data
	log  *log.Helper
}

// NewUserRepo .
func NewUserRepo(data *Data, logger log.Logger, kafka *KafkaProducer) biz.UserRepo {
	return &userRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

// 实现repo接口
func (r *userRepo) CreateUser(ctx context.Context, u *biz.User) (*biz.User, error) {
	var user models.User
	res := r.data.Db.Where("id=?", u.ID).Find(&user)
	if res.RowsAffected > 0 {
		return nil, errors.New("用户已存在")
	}
	res = r.data.Db.Create(&u)
	if res.Error != nil {
		return nil, res.Error
	}
	return u, nil
}

func (r *userRepo) Login(ctx context.Context, u *biz.User) (*biz.User, error) {
	var user models.User
	res := r.data.Db.Where("id=?", u.ID).Find(&user)
	if res.RowsAffected == 0 {
		return nil, errors.New("用户不存在")
	}
	if user.PasswordHash != u.Password {
		return nil, errors.New("密码错误")
	}
	return u, nil
}

func (r *userRepo) UpdateUser(ctx context.Context, u *biz.User) error {
	var user models.User
	res := r.data.Db.Where("id=?", u.ID).Find(&user)
	if res.RowsAffected == 0 {
		return errors.New("用户不存在")
	}
	user.Nickname = u.Nickname
	user.PasswordHash = u.Password
	user.Bio = u.Bio
	user.Gender = u.Gender
	res = r.data.Db.Save(&user)
	if res.Error != nil {
		return res.Error
	}
	return nil
}

func (r *userRepo) GetUserProfile(ctx context.Context, id int64) (*biz.User, error) {
	var user models.User
	res := r.data.Db.Where("id=?", id).Find(&user)
	if res.RowsAffected == 0 {
		return nil, errors.New("用户不存在")
	}
	if res.Error != nil {
		return nil, res.Error
	}
	u := &biz.User{
		ID:             user.ID,
		Phone:          user.Phone,
		Nickname:       user.Nickname,
		Avater:         user.Avatar,
		Bio:            user.Bio,
		Gender:         user.Gender,
		Birthday:       user.Birthdate,
		FansCount:      user.FansCount,
		FollowingCount: user.FollowingCount,
	}
	return u, nil
}

func (r *userRepo) DeleteUser(ctx context.Context, id int64) error {
	var user models.User
	res := r.data.Db.Where("id=?", id).Find(&user)
	if res.RowsAffected == 0 {
		return errors.New("用户不存在")
	}
	if res.Error != nil {
		return res.Error
	}
	res = r.data.Db.Delete(&user)
	if res.Error != nil {
		return res.Error
	}
	return nil
}

func (r *userRepo) Follow(ctx context.Context, follow *biz.Follow) error {
	var user models.User
	res := r.data.Db.Where("id=?", follow.Follower_id).Find(&user)
	if res.RowsAffected == 0 {
		return errors.New("用户不存在")
	}
	if res.Error != nil {
		return res.Error
	}
	followed := models.Follow{
		FollowerID: strconv.FormatInt(follow.Follower_id, 10),
		FolloweeID: strconv.FormatInt(follow.Followed_id, 10),
		Status:     models.FollowStatusActive,
	}
	res = r.data.Db.Create(&followed)
	if res.Error != nil {
		return res.Error
	}
	return nil
}

func (r *userRepo) ListFollowers(ctx context.Context, id int64) ([]*biz.User, error) {
	var user models.User
	res := r.data.Db.Where("id=?", id).Find(&user)
	if res.RowsAffected == 0 {
		return nil, errors.New("用户不存在")
	}
	if res.Error != nil {
		return nil, res.Error
	}
	var followers []*biz.User
	res = r.data.Db.Model(&models.Follow{}).Where("followee_id=?", strconv.FormatInt(id, 10)).Find(&followers)
	if res.Error != nil {
		return nil, res.Error
	}
	return followers, nil

}

func (r *userRepo) ListFollowings(ctx context.Context, id int64) ([]*biz.User, error) {
	var user models.User
	res := r.data.Db.Where("id=?", id).Find(&user)
	if res.RowsAffected == 0 {
		return nil, errors.New("用户不存在")
	}
	if res.Error != nil {
		return nil, res.Error
	}
	var followings []*biz.User
	res = r.data.Db.Model(&models.Follow{}).Where("follower_id=?", strconv.FormatInt(id, 10)).Find(&followings)
	if res.Error != nil {
		return nil, res.Error
	}
	return followings, nil
}
