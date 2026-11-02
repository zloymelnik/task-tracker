CREATE TABLE team_members (
    team_id         BINARY(16)                      NOT NULL,
    user_id         BINARY(16)                      NOT NULL,
    role            ENUM('owner','admin','member')  NOT NULL DEFAULT 'member',
    joined_at       DATETIME(3)                     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (team_id, user_id),
    CONSTRAINT fk_team_members_team FOREIGN KEY (team_id) REFERENCES teams (id),
    CONSTRAINT fk_team_members_user FOREIGN KEY (user_id) REFERENCES users (id),
    KEY idx_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
