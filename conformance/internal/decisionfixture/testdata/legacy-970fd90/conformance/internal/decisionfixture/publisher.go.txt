package decisionfixture

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	v "github.com/ruipengliu/lerna/contract/v1_1"
)

func (s *Store) Publish(ctx context.Context, publicationKey string, body []byte, sources []v.ContentRef, p decision.Permission) (v.ContentRef, error) {
	var out v.ContentRef
	if publicationKey == "" || len(publicationKey) > 1024 || len(body) > v.MaxBodyBytes || len(sources) > 64 {
		return out, errors.New("fixture publication finite bound exceeded")
	}
	permissionID, err := permissionKey(p)
	if err != nil {
		return out, err
	}
	sourceBytes, err := jsonBytes(sources)
	if err != nil {
		return out, err
	}
	publicationDigest := digest(body)
	err = s.within(ctx, func(ctx context.Context, tx *sql.Tx, _ time.Time) error {
		// Sample permission time only after waiting for the original publication
		// lock; an expired permit cannot revive an existing publication retry.
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1786614102,hashtext($1))`, s.cfg.Schema+"/"+permissionID+"/"+publicationKey); err != nil {
			return err
		}
		g, err := s.current(ctx, tx, p, "publish")
		if err != nil {
			return err
		}
		for _, source := range sources {
			if !slices.Contains(g.ContentRefs, source) {
				return decision.ErrForbidden
			}
			sourceBody, err := s.object(ctx, tx, source, "material")
			if err != nil {
				return err
			}
			if err = checkContent(source, sourceBody); err != nil {
				return err
			}
		}
		var priorDigest string
		var priorRef, priorSources []byte
		err = tx.QueryRowContext(ctx, `SELECT digest,ref,sources FROM `+s.table("fixture_publications")+` WHERE permission_key=$1 AND publication_key=$2`, permissionID, publicationKey).Scan(&priorDigest, &priorRef, &priorSources)
		if err == nil {
			if priorDigest != publicationDigest || !bytes.Equal(priorSources, sourceBytes) {
				return decision.ErrPublicationConflict
			}
			if err = closedJSON(priorRef, &out); err != nil {
				return decision.ErrUnavailable
			}
			stored, err := s.object(ctx, tx, out, "published")
			if err != nil {
				return err
			}
			if !bytes.Equal(stored, body) {
				return decision.ErrPublicationConflict
			}
			return checkContent(out, stored)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// The content identity is derived from the original grant and key, never a
		// new random retry identity or from the body's digest.
		identity := strings.TrimPrefix(digest([]byte(permissionID+"/"+publicationKey)), "sha256:")
		media := "text/plain"
		if _, err := v.ParseJSON(body); err == nil {
			media = "application/json"
		}
		out = s.Ref(v.ID("publication-"+identity[:40]), media, body)
		refBytes, err := v.Encode(out)
		if err != nil {
			return err
		}
		objectKey, err := key(out)
		if err != nil {
			return err
		}
		if err = s.immutable(ctx, tx, "fixture_objects", "object_key", objectKey, "published", body); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO `+s.table("fixture_publications")+`(permission_key,publication_key,digest,ref,sources) VALUES($1,$2,$3,$4,$5)`, permissionID, publicationKey, publicationDigest, refBytes, sourceBytes)
		return err
	})
	return out, err
}
func (s *Store) ReadPublished(ctx context.Context, ref v.ContentRef, p decision.Permission) ([]byte, error) {
	var out []byte
	if _, err := v.Encode(ref); err != nil {
		return out, err
	}
	permissionID, err := permissionKey(p)
	if err != nil {
		return out, err
	}
	err = s.within(ctx, func(ctx context.Context, tx *sql.Tx, _ time.Time) error {
		if _, err := s.current(ctx, tx, p, "publish"); err != nil {
			return err
		}
		refBytes, err := v.Encode(ref)
		if err != nil {
			return err
		}
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.table("fixture_publications")+` WHERE permission_key=$1 AND ref=$2)`, permissionID, refBytes).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return decision.ErrForbidden
		}
		out, err = s.object(ctx, tx, ref, "published")
		if err != nil {
			return err
		}
		return checkContent(ref, out)
	})
	return out, err
}
