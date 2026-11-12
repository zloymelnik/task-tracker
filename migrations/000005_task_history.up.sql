CREATE TABLE task_history (
    id              BINARY(16)  NOT NULL,
    task_id         BINARY(16)  NOT NULL,
    changed_by      BINARY(16)  NOT NULL,
    changes         JSON        NOT NULL,
    created_at      DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    CONSTRAINT fk_task_history_task_id      FOREIGN KEY (task_id)       REFERENCES tasks (id),
    CONSTRAINT fk_task_history_changed_by   FOREIGN KEY (changed_by)    REFERENCES users (id),
    KEY idx_task_history_task_created (task_id, created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
