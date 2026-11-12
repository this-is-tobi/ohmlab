package rustfs

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/minio/madmin-go/v4"
)

// User is what User reads of a store user.
type User struct {
	Exists bool
	// Policies are the canned policies attached to the user, sorted.
	Policies []string
}

// Policy returns a canned policy's document, or nil when there is none.
func (c *Client) Policy(ctx context.Context, name string) ([]byte, error) {
	info, err := c.admin.InfoCannedPolicy(ctx, name)
	if err := wrapErr("policy "+name, err); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return info.Policy, nil
}

// PutPolicy creates or replaces a canned policy.
func (c *Client) PutPolicy(ctx context.Context, name string, doc []byte) error {
	return wrapErr("put-policy "+name, c.admin.AddCannedPolicy(ctx, name, doc))
}

// RemovePolicy removes a canned policy. The store refuses while it is
// attached. A missing one is removed already.
func (c *Client) RemovePolicy(ctx context.Context, name string) error {
	return gone(wrapErr("remove-policy "+name, c.admin.RemoveCannedPolicy(ctx, name)))
}

// User reads a user. A missing user is Exists false, with no error.
func (c *Client) User(ctx context.Context, name string) (User, error) {
	info, err := c.admin.GetUserInfo(ctx, name)
	if err := wrapErr("user "+name, err); err != nil {
		if errors.Is(err, ErrNotFound) {
			return User{}, nil
		}
		return User{}, err
	}
	u := User{Exists: true}
	for p := range strings.SplitSeq(info.PolicyName, ",") {
		if p = strings.TrimSpace(p); p != "" {
			u.Policies = append(u.Policies, p)
		}
	}
	slices.Sort(u.Policies)
	return u, nil
}

// CreateUser creates a user, or gives an existing one this secret.
func (c *Client) CreateUser(ctx context.Context, name, secret string) error {
	return wrapErr("create-user "+name, c.admin.AddUser(ctx, name, secret))
}

// AttachPolicy attaches a canned policy to a user. Attached already is done.
func (c *Client) AttachPolicy(ctx context.Context, policy, user string) error {
	_, err := c.admin.AttachPolicy(ctx, madmin.PolicyAssociationReq{Policies: []string{policy}, User: user})
	return applied(wrapErr("attach-policy "+policy+" to "+user, err), err)
}

// DetachPolicy detaches a canned policy from a user. Detached already is done.
func (c *Client) DetachPolicy(ctx context.Context, policy, user string) error {
	_, err := c.admin.DetachPolicy(ctx, madmin.PolicyAssociationReq{Policies: []string{policy}, User: user})
	return applied(wrapErr("detach-policy "+policy+" from "+user, err), err)
}

// RemoveUser removes a user. Remove its service accounts first. A missing
// user is removed already.
func (c *Client) RemoveUser(ctx context.Context, name string) error {
	return gone(wrapErr("remove-user "+name, c.admin.RemoveUser(ctx, name)))
}

// ServiceAccounts returns the access keys of a user's service accounts,
// sorted. A missing user has none.
func (c *Client) ServiceAccounts(ctx context.Context, user string) ([]string, error) {
	resp, err := c.admin.ListServiceAccounts(ctx, user)
	if err := wrapErr("service-accounts "+user, err); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	keys := make([]string, 0, len(resp.Accounts))
	for _, a := range resp.Accounts {
		keys = append(keys, a.AccessKey)
	}
	slices.Sort(keys)
	return keys, nil
}

// AddServiceAccount adds a key to a user. It inherits the user's policies.
func (c *Client) AddServiceAccount(ctx context.Context, user, accessKey, secretKey string) error {
	_, err := c.admin.AddServiceAccount(ctx, madmin.AddServiceAccountReq{TargetUser: user, AccessKey: accessKey, SecretKey: secretKey})
	return wrapErr("add-service-account to "+user, err)
}

// RemoveServiceAccount removes a key. A missing one is removed already.
func (c *Client) RemoveServiceAccount(ctx context.Context, accessKey string) error {
	return gone(wrapErr("remove-service-account", c.admin.DeleteServiceAccount(ctx, accessKey)))
}

// gone treats ErrNotFound as done.
func gone(err error) error {
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// applied treats an association that is in place already as done.
func applied(wrapped, raw error) error {
	if raw != nil && strings.Contains(madmin.ToErrorResponse(raw).Code, "PolicyChangeAlreadyApplied") {
		return nil
	}
	return wrapped
}
