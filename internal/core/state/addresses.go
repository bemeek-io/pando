package state

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/trypando/pando/internal/core/spec"
	"github.com/trypando/pando/internal/errs"
)

// Where each app is reached: apps.address_hostname and apps.address_path,
// written when a revision is pinned (migration 35).

// querier is what an address check reads through: the pool, or the pin's
// transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// addressOf is the hostname and path a routing block claims. At most one is
// set; a port-mode app claims neither (its port is port_allocations').
func addressOf(r spec.Routing) (hostname, path string) {
	switch r.Mode {
	case spec.RoutingSubdomain:
		return strings.ToLower(r.Hostname), ""
	case spec.RoutingPath:
		return "", strings.ToLower(r.PathPrefix)
	}
	return "", ""
}

// CheckAddress reports whether another live app already holds the address r
// would claim for appID, saying which address and why. nil means it is free.
//
// Asked before a change is saved, so the person choosing hears it then, and
// again inside Pin, which is where it is decided: the unique indexes refuse
// an exact duplicate there however two writers race, and this refuses the
// rest — a path inside or around another app's, or one that takes another
// app's slug.
func (a *Apps) CheckAddress(ctx context.Context, appID string, r spec.Routing) error {
	return checkAddress(ctx, a.db, appID, r)
}

func checkAddress(ctx context.Context, q querier, appID string, r spec.Routing) error {
	hostname, path := addressOf(r)

	if hostname != "" {
		var other string
		err := q.QueryRow(ctx, `
			SELECT name FROM apps
			WHERE deleted_at IS NULL AND id <> $1 AND address_hostname = $2
			LIMIT 1`, appID, hostname).Scan(&other)
		if err == nil {
			return taken(hostname, other, "is already reached at it")
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return errs.Wrap(errs.Internal, "Could not check whether the address is free.", err)
		}
	}

	if path != "" {
		// The same path, or one inside or around it on a whole-segment
		// boundary: /team and /team/notes cannot both be apps, since one
		// would receive the other's requests, and an app claiming a path
		// under another's is a page in that app's name it does not control.
		var other, otherPath string
		err := q.QueryRow(ctx, `
			SELECT name, address_path FROM apps
			WHERE deleted_at IS NULL AND id <> $1 AND address_path IS NOT NULL
			  AND (address_path = $2
			       OR starts_with($2, address_path || '/')
			       OR starts_with(address_path, $2 || '/'))
			LIMIT 1`, appID, path).Scan(&other, &otherPath)
		if err == nil {
			switch {
			case otherPath == path:
				return taken(path, other, "is already reached at it")
			case strings.HasPrefix(path, otherPath+"/"):
				return taken(path, other, "is reached at "+otherPath+", which this path is inside")
			default:
				return taken(path, other, "is reached at "+otherPath+", which is inside this path")
			}
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return errs.Wrap(errs.Internal, "Could not check whether the address is free.", err)
		}

		// Another app's slug as the first segment. Every app answers at
		// /<slug> as well as its own path (proxy.resolve), and a path that
		// took one would take that app's requests.
		first := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)[0]
		err = q.QueryRow(ctx, `
			SELECT name FROM apps
			WHERE deleted_at IS NULL AND id <> $1 AND slug = $2
			LIMIT 1`, appID, first).Scan(&other)
		if err == nil {
			return taken(path, other, "is reached at /"+first)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return errs.Wrap(errs.Internal, "Could not check whether the address is free.", err)
		}
	}
	return nil
}

func taken(address, other, why string) error {
	return errs.Newf(errs.StateAddressTaken, "%s can't be this app's address: %s %s.", address, other, why).
		WithRemedy("Choose another address for this app, or change the other app's first.")
}

// ByPath resolves the path-mode app whose path is the longest prefix of the
// request's path, on whole segments, and returns that path.
//
// Case-sensitive, as a URL's path is: paths are stored lowercase, and the
// proxy strips exactly the prefix that matched.
func (a *Apps) ByPath(ctx context.Context, requestPath string) (App, *spec.AppSpec, string, bool, error) {
	p := requestPath
	var appID, prefix string
	err := a.db.QueryRow(ctx, `
		SELECT id, address_path FROM apps
		WHERE deleted_at IS NULL AND address_path IS NOT NULL
		  AND ($1 = address_path OR starts_with($1, address_path || '/'))
		ORDER BY length(address_path) DESC
		LIMIT 1`, p).Scan(&appID, &prefix)
	if errors.Is(err, pgx.ErrNoRows) {
		return App{}, nil, "", false, nil
	}
	if err != nil {
		return App{}, nil, "", false, errs.Wrap(errs.Internal, "Could not look up the app for this path.", err)
	}
	app, s, found, err := a.ByRouting(ctx, "id", appID)
	return app, s, prefix, found, err
}
