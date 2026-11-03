CREATE TABLE tasks (
    id              BINARY(16)                          NOT NULL,
    team_id         BINARY(16)                          NOT NULL,
    created_by      BINARY(16)                          NOT NULL,
    assignee_id     BINARY(16)                          NULL,
    title           VARCHAR(255)                        NOT NULL,
    description     TEXT                                NOT NULL,
    status          ENUM('todo','in_progress','done')   NOT NULL DEFAULT 'todo',
    created_at      DATETIME(3)                         NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at      DATETIME(3)                         NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    closed_at       DATETIME(3)                         NULL,
    version         INT UNSIGNED                        NOT NULL DEFAULT 1,
    PRIMARY KEY (id),
    CONSTRAINT fk_tasks_team_id     FOREIGN KEY (team_id)       REFERENCES teams (id),
    CONSTRAINT fk_tasks_created_by  FOREIGN KEY (created_by)    REFERENCES users (id),
    CONSTRAINT fk_tasks_assignee_id FOREIGN KEY (assignee_id)   REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
