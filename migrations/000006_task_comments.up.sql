CREATE TABLE task_comments (
    id         BINARY(16)  NOT NULL,
    task_id    BINARY(16)  NOT NULL,
    created_by BINARY(16)  NOT NULL,
    body       TEXT        NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    CONSTRAINT fk_task_comments_task FOREIGN KEY (task_id) REFERENCES tasks (id),
    CONSTRAINT fk_task_comments_user FOREIGN KEY (created_by) REFERENCES users (id),
    KEY idx_task_comments_task_created (task_id, created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;