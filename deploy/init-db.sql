-- Evolution GO Custom — database bootstrap.
-- Runs automatically the first time the Postgres data volume is initialized.
-- Creates the two databases the application connects to.

CREATE DATABASE evogo_auth;
CREATE DATABASE evogo_users;
