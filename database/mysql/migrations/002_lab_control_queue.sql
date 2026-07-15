USE platform;

CREATE TABLE IF NOT EXISTS lab_control_locks (
  lock_name VARCHAR(64) NOT NULL,
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
    ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (lock_name)
) ENGINE=InnoDB;

INSERT INTO lab_control_locks (lock_name)
VALUES ('global-admission')
ON DUPLICATE KEY UPDATE lock_name = VALUES(lock_name);
