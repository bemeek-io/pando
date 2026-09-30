package idp

import (
	"context"

	"github.com/trypando/pando/internal/core/authz"
	"github.com/trypando/pando/internal/core/state"
	"github.com/trypando/pando/internal/errs"
)

// Linking an identity to an account, and a provider's group to a Pando group
// (O-1, R-078). Both add; neither merges.

// LinkIdentity attaches a provider's identity to an account. When it already
// reaches another account, it moves only with replace set, and that account
// becomes a suspended alias of this one — never deleted (design 02 §2.1).
func (s *Service) LinkIdentity(ctx context.Context, p authz.Principal, userID, providerID, externalID string, replace bool) (string, error) {
	aliased, err := s.Identities.Link(ctx, userID, providerID, externalID, p.ID, replace)
	if err != nil {
		return "", err
	}
	detail := map[string]any{"adapter_id": providerID, "external_id": externalID}
	if aliased != "" {
		detail["aliased_user_id"] = aliased
	}
	s.audit(ctx, principalEvent(p, "user.identity.link", "user", userID, detail))
	return aliased, nil
}

// UnlinkIdentity removes a provider's identity from an account. The account
// stays; whoever signs in with that identity next is a stranger again.
func (s *Service) UnlinkIdentity(ctx context.Context, p authz.Principal, userID, providerID, externalID string) error {
	if err := s.Identities.Unlink(ctx, userID, providerID, externalID); err != nil {
		return err
	}
	s.audit(ctx, principalEvent(p, "user.identity.unlink", "user", userID,
		map[string]any{"adapter_id": providerID, "external_id": externalID}))
	return nil
}

// Identities lists the identities that reach an account.
func (s *Service) IdentitiesFor(ctx context.Context, userID string) ([]state.Identity, error) {
	return s.Identities.ForUser(ctx, userID)
}

// LinkGroup makes everyone in a provider's group count as a member of a
// Pando-made group, live (R-079).
func (s *Service) LinkGroup(ctx context.Context, p authz.Principal, groupID, syncedGroupID string) error {
	if err := s.Groups.Link(ctx, syncedGroupID, groupID, p.ID); err != nil {
		return err
	}
	s.audit(ctx, principalEvent(p, "group.link", "group", groupID, map[string]any{"synced_group_id": syncedGroupID}))
	return nil
}

// UnlinkGroup removes such a link. Its members lose what the Pando group gave
// them, within the revocation window (design 06 §3.1).
func (s *Service) UnlinkGroup(ctx context.Context, p authz.Principal, groupID, syncedGroupID string) error {
	if groupID == "" || syncedGroupID == "" {
		return errs.New(errs.ValidInvalid, "Name both groups.")
	}
	if err := s.Groups.Unlink(ctx, syncedGroupID, groupID); err != nil {
		return err
	}
	s.audit(ctx, principalEvent(p, "group.unlink", "group", groupID, map[string]any{"synced_group_id": syncedGroupID}))
	return nil
}
