package access_service

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

	access_repository "github.com/EvolutionAPI/evolution-go/pkg/access/repository"
	"github.com/go-ldap/ldap/v3"
)

// Chaves em evogo_settings (editáveis pela UI: Admin → Settings).
const (
	settingLdapEnabled      = "ldap.enabled"
	settingLdapURL          = "ldap.url"           // ex: ldaps://ad.dominio.local:636 ou ldap://ad.dominio.local:389
	settingLdapBindDN       = "ldap.bind_dn"        // conta de serviço, ex: CN=svc-evogo,OU=...,DC=...
	settingLdapBindPassword = "ldap.bind_password"  // senha da conta de serviço
	settingLdapBaseDN       = "ldap.base_dn"        // base de busca, ex: OU=GrupoNewland,DC=...
	settingLdapUserFilter   = "ldap.user_filter"    // ex: (sAMAccountName=%s)
	settingLdapGroupAttr    = "ldap.group_attribute" // ex: memberOf
	settingLdapStartTLS     = "ldap.start_tls"       // "true" para ldap:// + StartTLS
	settingLdapSkipVerify   = "ldap.skip_verify_tls" // "true" para aceitar cert self-signed/wildcard
)

var ErrLdapDisabled = errors.New("LDAP não está habilitado")

type LdapConfig struct {
	Enabled      bool
	URL          string
	BindDN       string
	BindPassword string
	BaseDN       string
	UserFilter   string
	GroupAttr    string
	StartTLS     bool
	SkipVerify   bool
}

func defaultLdapConfig() LdapConfig {
	return LdapConfig{
		UserFilter: "(sAMAccountName=%s)",
		GroupAttr:  "memberOf",
	}
}

func loadLdapConfig(repo access_repository.AccessRepository) (LdapConfig, error) {
	settings, err := repo.ListSettings()
	if err != nil {
		return LdapConfig{}, err
	}
	cfg := defaultLdapConfig()
	cfg.Enabled = settings[settingLdapEnabled] == "true"
	cfg.URL = settings[settingLdapURL]
	cfg.BindDN = settings[settingLdapBindDN]
	cfg.BindPassword = settings[settingLdapBindPassword]
	cfg.BaseDN = settings[settingLdapBaseDN]
	cfg.StartTLS = settings[settingLdapStartTLS] == "true"
	cfg.SkipVerify = settings[settingLdapSkipVerify] == "true"
	if v := settings[settingLdapUserFilter]; v != "" {
		cfg.UserFilter = v
	}
	if v := settings[settingLdapGroupAttr]; v != "" {
		cfg.GroupAttr = v
	}
	return cfg, nil
}

func (cfg LdapConfig) validate() error {
	if cfg.URL == "" || cfg.BindDN == "" || cfg.BaseDN == "" {
		return errors.New("configuração LDAP incompleta: url, bind_dn e base_dn são obrigatórios")
	}
	return nil
}

func dialLdap(cfg LdapConfig) (*ldap.Conn, error) {
	var conn *ldap.Conn
	var err error
	if cfg.SkipVerify {
		conn, err = ldap.DialURL(cfg.URL, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: true}))
	} else {
		conn, err = ldap.DialURL(cfg.URL)
	}
	if err != nil {
		return nil, fmt.Errorf("falha ao conectar no LDAP (%s): %w", cfg.URL, err)
	}
	if cfg.StartTLS {
		tlsConfig := &tls.Config{InsecureSkipVerify: cfg.SkipVerify}
		if err := conn.StartTLS(tlsConfig); err != nil {
			conn.Close()
			return nil, fmt.Errorf("falha no StartTLS: %w", err)
		}
	}
	return conn, nil
}

// searchUser faz bind com a conta de serviço e busca o DN + grupos do usuário.
func searchUser(cfg LdapConfig, conn *ldap.Conn, username string) (dn string, groupDNs []string, displayName string, err error) {
	if err = conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
		return "", nil, "", fmt.Errorf("bind da conta de serviço falhou: %w", err)
	}
	filter := fmt.Sprintf(cfg.UserFilter, ldap.EscapeFilter(username))
	req := ldap.NewSearchRequest(
		cfg.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		filter,
		[]string{"dn", cfg.GroupAttr, "displayName", "cn"},
		nil,
	)
	result, err := conn.Search(req)
	if err != nil {
		return "", nil, "", fmt.Errorf("busca LDAP falhou: %w", err)
	}
	if len(result.Entries) == 0 {
		return "", nil, "", errors.New("usuário não encontrado no LDAP")
	}
	entry := result.Entries[0]
	dn = entry.DN
	groupDNs = entry.GetAttributeValues(cfg.GroupAttr)
	displayName = entry.GetAttributeValue("displayName")
	if displayName == "" {
		displayName = entry.GetAttributeValue("cn")
	}
	return dn, groupDNs, displayName, nil
}

// AuthenticateLdap: bind da conta de serviço → localiza o usuário → re-bind com a senha dele.
// Retorna o DN, o nome de exibição e os DNs de grupo (memberOf) pra sincronizar depois.
func AuthenticateLdap(repo access_repository.AccessRepository, username, password string) (dn, displayName string, groupDNs []string, err error) {
	cfg, err := loadLdapConfig(repo)
	if err != nil {
		return "", "", nil, err
	}
	if !cfg.Enabled {
		return "", "", nil, ErrLdapDisabled
	}
	if err := cfg.validate(); err != nil {
		return "", "", nil, err
	}

	conn, err := dialLdap(cfg)
	if err != nil {
		return "", "", nil, err
	}
	defer conn.Close()

	userDN, groups, name, err := searchUser(cfg, conn, username)
	if err != nil {
		return "", "", nil, err
	}

	// Re-bind com as credenciais do próprio usuário — é isso que de fato autentica a senha.
	userConn, err := dialLdap(cfg)
	if err != nil {
		return "", "", nil, err
	}
	defer userConn.Close()
	if err := userConn.Bind(userDN, password); err != nil {
		return "", "", nil, errors.New("usuário ou senha inválidos")
	}

	return userDN, name, groups, nil
}

// TestLdapSettings faz apenas o bind da conta de serviço (sem usuário) — usado pelo botão "Testar conexão".
func TestLdapSettings(repo access_repository.AccessRepository) error {
	cfg, err := loadLdapConfig(repo)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return ErrLdapDisabled
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	conn, err := dialLdap(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
		return fmt.Errorf("bind da conta de serviço falhou: %w", err)
	}
	return nil
}

// matchGroupDNs: quais AccessGroup.LdapGroupDN aparecem nos groupDNs do usuário (comparação case-insensitive).
func matchGroupDNs(groupDNs []string, ldapGroupDN string) bool {
	if ldapGroupDN == "" {
		return false
	}
	for _, dn := range groupDNs {
		if strings.EqualFold(strings.TrimSpace(dn), strings.TrimSpace(ldapGroupDN)) {
			return true
		}
	}
	return false
}
