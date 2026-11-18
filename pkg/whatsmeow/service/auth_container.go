package whatsmeow_service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// Container único das sessões do WhatsApp (tabelas whatsmeow_*).
//
// Antes, cada StartClient chamava sqlstore.New, que abre um pool novo de
// conexões e nunca o fechava: cada conexão/reconexão de instância deixava
// mais conexões abertas no Postgres. Agora o processo inteiro usa um único
// container, montado sobre o pool que o main.go já abre e limita
// (POSTGRES_AUTH_MAX_OPEN / POSTGRES_AUTH_MAX_IDLE).
//
// A atualização do schema (Upgrade) tem prazo: se o banco aceitar TCP mas
// não responder, desiste em authUpgradeTimeout em vez de travar todos os
// StartClient. Falha não fica em cache: a próxima chamada tenta de novo.

const authUpgradeTimeout = 60 * time.Second

var (
	authContainerMu sync.Mutex
	authContainer   *sqlstore.Container
)

func (w whatsmeowService) getAuthContainer() (*sqlstore.Container, error) {
	authContainerMu.Lock()
	defer authContainerMu.Unlock()
	if authContainer != nil {
		return authContainer, nil
	}

	var dbLog waLog.Logger
	if w.config.WaDebug != "" {
		dbLog = waLog.Stdout("Database", w.config.WaDebug, true)
	}

	ctx, cancel := context.WithTimeout(context.Background(), authUpgradeTimeout)
	defer cancel()

	var (
		c   *sqlstore.Container
		err error
	)
	switch {
	case w.config.PostgresAuthDB != "" && w.authDB != nil:
		c = sqlstore.NewWithDB(w.authDB, "postgres", dbLog)
		if err = c.Upgrade(ctx); err != nil {
			return nil, fmt.Errorf("falha ao atualizar o banco de sessões: %w", err)
		}
	case w.config.PostgresAuthDB != "":
		c, err = sqlstore.New(ctx, "postgres", w.config.PostgresAuthDB, dbLog)
	default:
		dsn := fmt.Sprintf("file:%s/dbdata/main.db?_pragma=foreign_keys(1)&_busy_timeout=5000&cache=shared&mode=rwc&_journal_mode=WAL", w.exPath)
		c, err = sqlstore.New(ctx, "sqlite", dsn, dbLog)
	}
	if err != nil {
		return nil, err
	}
	authContainer = c
	return c, nil
}
