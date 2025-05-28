package data

import (
	"context"
	"errors"
	"time"
	"user/internal/biz"
)



func (r *userRepo) SendVerificationCode(ctx context.Context, code *biz.Code) error {
	res:=r.data.RDb.Set(ctx, code.Phone, code.VerifyCode, 5*time.Minute)
	if res.Err() != nil {
		return res.Err()
	}
	return nil
}

func (r *userRepo) VerifyCode(ctx context.Context, code *biz.Code) ( error) {
	res := r.data.RDb.Get(ctx, code.Phone)
	if res.Err() != nil {
		return res.Err()
	}
	if res.Val() == code.VerifyCode {
		return errors.New("验证码不存在或已过期")
	}
	return nil
}