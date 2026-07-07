-- v14 (compatible with v8+): Add NCT salt table for <cstoken> derivation (fixes error 463 on cold contacts)
CREATE TABLE whatsmeow_nct_salt (
	our_jid TEXT  NOT NULL,
	salt    bytea NOT NULL,
	PRIMARY KEY (our_jid)
);
