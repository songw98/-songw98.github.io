package store

import (
	"context"
	"time"

	"github.com/shellhub-io/shellhub/pkg/api/paginator"
	"github.com/shellhub-io/shellhub/pkg/models"
)

type SessionStore interface {
	SessionList(ctx context.Context, pagination paginator.Query) ([]models.Session, int, error)
	SessionGet(ctx context.Context, uid models.UID) (*models.Session, error)
	SessionCreate(ctx context.Context, session models.Session) (*models.Session, error)
	SessionSetAuthenticated(ctx context.Context, uid models.UID, authenticated bool) error
	SessionSetLastSeen(ctx context.Context, uid models.UID) error
	SessionDeleteActives(ctx context.Context, uid models.UID) error
	SessionCreateRecordFrame(ctx context.Context, uid models.UID, recordSession *models.RecordedSession) error
	SessionUpdateDeviceUID(ctx context.Context, oldUID models.UID, newUID models.UID) error
	SessionGetRecordFrame(ctx context.Context, uid models.UID) ([]models.RecordedSession, int, error)
	SessionDeleteRecordFrame(ctx context.Context, uid models.UID) error
	SessionDeleteRecordFrameByDate(ctx context.Context, lte time.Time) (deletedCount int64, updatedCount int64, err error)
	SessionSetRecorded(ctx context.Context, uid models.UID, recorded bool) error
}
