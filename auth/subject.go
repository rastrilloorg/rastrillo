package auth

import "context"

func (auth *Auth) subjectFor(
	ctx context.Context, address string,
) (string, error) {
	if auth.cfg.BlindLinks != nil {
		return auth.blindSubject(ctx, address)
	}
	if auth.users != nil {
		user, err := auth.users.ensure(ctx, address)
		if err != nil {
			return "", err
		}
		return user.ID, nil
	}
	if auth.cfg.SubjectFor != nil {
		return auth.cfg.SubjectFor(address)
	}
	return address, nil
}
