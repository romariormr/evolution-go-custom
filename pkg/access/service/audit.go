package access_service

import (
	"os"
	"strconv"
	"strings"

	access_model "github.com/EvolutionAPI/evolution-go/pkg/access/model"
	access_repository "github.com/EvolutionAPI/evolution-go/pkg/access/repository"
)

// Ações registradas na auditoria (evogo_audit_log).
const (
	AuditLogin          = "login"
	AuditLoginFailed    = "login_failed"
	AuditLogout         = "logout"
	AuditSessionRevoked = "session_revoked"
	AuditPasswordChange = "password_changed"
	AuditPasswordReset  = "password_reset"
	Audit2FAEnabled     = "2fa_enabled"
	Audit2FADisabled    = "2fa_disabled"
	Audit2FAReset       = "2fa_reset"
	AuditUserCreated    = "user_created"
	AuditUserUpdated    = "user_updated"
	AuditUserDeleted    = "user_deleted"
	AuditUserGroups     = "user_groups_changed"
	AuditGroupCreated   = "group_created"
	AuditGroupDeleted   = "group_deleted"
	AuditGroupKey       = "group_key_regenerated"
	AuditInstanceShared = "instance_shared"
	AuditInstanceUnshar = "instance_unshared"
	AuditInstanceCreate = "instance_created"
	AuditInstanceDelete = "instance_deleted"
	AuditSettingChanged = "setting_changed"
)

func auditRetentionDays() int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("AUDIT_RETENTION_DAYS"))); err == nil && v > 0 {
		return v
	}
	return 180
}

// Audit grava um evento. Falha ao gravar não interrompe a operação.
func (s *accessService) Audit(actor *access_model.AccessUser, ci ClientInfo, action, target, detail string, success bool) {
	a := &access_model.AccessAudit{
		IP:      ci.IP,
		Action:  action,
		Target:  clipStr(target, 200),
		Detail:  clipStr(detail, 500),
		Success: success,
	}
	if actor != nil {
		a.UserId = actor.Id
		a.Username = actor.Username
	}
	_ = s.repo.AddAudit(a)
}

// AuditAs grava evento sem usuário carregado (ex.: falha de login) usando só o nome digitado.
func AuditUsername(username string) *access_model.AccessUser {
	return &access_model.AccessUser{Username: strings.ToLower(strings.TrimSpace(username))}
}

func (s *accessService) ListAudit(f access_repository.AuditFilter) ([]*access_model.AccessAudit, int64, error) {
	return s.repo.ListAudit(f)
}
