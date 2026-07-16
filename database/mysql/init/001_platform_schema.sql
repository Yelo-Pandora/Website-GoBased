CREATE DATABASE IF NOT EXISTS platform
  CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;

USE platform;

CREATE TABLE IF NOT EXISTS users (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  username VARCHAR(64) NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
    ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uk_users_username (username),
  CONSTRAINT chk_users_status
    CHECK (status IN ('active', 'disabled'))
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS auth_sessions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NOT NULL,
  session_token_hash BINARY(32) NOT NULL,
  csrf_token_hash BINARY(32) NOT NULL,
  expires_at DATETIME(6) NOT NULL,
  last_seen_at DATETIME(6) NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  revoked_at DATETIME(6) NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_auth_sessions_token (session_token_hash),
  KEY idx_auth_sessions_user_expires (user_id, expires_at),
  CONSTRAINT fk_auth_sessions_user
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS courses (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  slug VARCHAR(128) NOT NULL,
  title VARCHAR(160) NOT NULL,
  category VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL,
  sort_order INT NOT NULL,
  summary TEXT NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
    ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uk_courses_slug (slug),
  KEY idx_courses_status_sort (status, sort_order),
  CONSTRAINT chk_courses_status
    CHECK (status IN ('theory', 'active', 'coming_soon'))
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS course_progress (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NOT NULL,
  course_id BIGINT UNSIGNED NOT NULL,
  viewed BOOLEAN NOT NULL DEFAULT FALSE,
  last_viewed_at DATETIME(6) NULL,
  last_lab_id VARCHAR(64) NULL,
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
    ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uk_course_progress_user_course (user_id, course_id),
  CONSTRAINT fk_course_progress_user
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_course_progress_course
    FOREIGN KEY (course_id) REFERENCES courses (id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS lab_sessions (
  id VARCHAR(64) NOT NULL,
  user_id BIGINT UNSIGNED NOT NULL,
  course_id BIGINT UNSIGNED NOT NULL,
  scenario_type VARCHAR(64) NOT NULL,
  scenario_template_id VARCHAR(128) NOT NULL,
  status VARCHAR(32) NOT NULL,
  balancing_mode VARCHAR(32) NOT NULL DEFAULT 'fixed',
  mysql_db_name VARCHAR(64) NULL,
  mysql_db_user VARCHAR(64) NULL,
  redis_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  started_at DATETIME(6) NULL,
  last_effective_action_at DATETIME(6) NULL,
  terminated_at DATETIME(6) NULL,
  termination_reason VARCHAR(128) NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
    ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  KEY idx_lab_sessions_user_status (user_id, status),
  KEY idx_lab_sessions_status_activity (status, last_effective_action_at),
  CONSTRAINT fk_lab_sessions_user
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT fk_lab_sessions_course
    FOREIGN KEY (course_id) REFERENCES courses (id) ON DELETE RESTRICT,
  CONSTRAINT chk_lab_sessions_status CHECK (
    status IN (
      'Preparing',
      'Running',
      'Expiring',
      'Failed',
      'Terminating',
      'Terminated'
    )
  ),
  CONSTRAINT chk_lab_sessions_balancing_mode
    CHECK (balancing_mode IN ('fixed', 'adaptive'))
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS lab_instances (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  lab_id VARCHAR(64) NOT NULL,
  instance_name VARCHAR(64) NOT NULL,
  container_id VARCHAR(128) NULL,
  status VARCHAR(32) NOT NULL,
  cpu_limit_cores DECIMAL(6, 3) NOT NULL,
  memory_limit_mb INT NOT NULL,
  performance_percent INT NOT NULL,
  effective_capacity INT NOT NULL,
  current_weight INT NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
    ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uk_lab_instances_name (lab_id, instance_name),
  UNIQUE KEY uk_lab_instances_container (container_id),
  KEY idx_lab_instances_lab_status (lab_id, status),
  CONSTRAINT fk_lab_instances_lab
    FOREIGN KEY (lab_id) REFERENCES lab_sessions (id) ON DELETE CASCADE,
  CONSTRAINT chk_lab_instances_performance
    CHECK (performance_percent BETWEEN 20 AND 100),
  CONSTRAINT chk_lab_instances_memory
    CHECK (memory_limit_mb > 0),
  CONSTRAINT chk_lab_instances_capacity
    CHECK (effective_capacity >= 0),
  CONSTRAINT chk_lab_instances_weight
    CHECK (current_weight > 0)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS lab_resources (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  lab_id VARCHAR(64) NOT NULL,
  resource_type VARCHAR(32) NOT NULL,
  resource_name VARCHAR(128) NOT NULL,
  external_id VARCHAR(128) NULL,
  status VARCHAR(32) NOT NULL,
  metadata_json JSON NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
    ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uk_lab_resources_name (lab_id, resource_type, resource_name),
  KEY idx_lab_resources_external (resource_type, external_id),
  CONSTRAINT fk_lab_resources_lab
    FOREIGN KEY (lab_id) REFERENCES lab_sessions (id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS lab_operations (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  operation_id VARCHAR(64) NOT NULL,
  lab_id VARCHAR(64) NOT NULL,
  requested_by BIGINT UNSIGNED NOT NULL,
  action VARCHAR(64) NOT NULL,
  target_instance_id VARCHAR(64) NULL,
  status VARCHAR(32) NOT NULL,
  payload_json JSON NULL,
  result_json JSON NULL,
  error_code VARCHAR(64) NULL,
  error_message TEXT NULL,
  lease_owner VARCHAR(128) NULL,
  lease_expires_at DATETIME(6) NULL,
  attempt_count INT NOT NULL DEFAULT 0,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
    ON UPDATE CURRENT_TIMESTAMP(6),
  completed_at DATETIME(6) NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_lab_operations_operation_id (operation_id),
  KEY idx_lab_operations_claim (status, lease_expires_at, created_at),
  KEY idx_lab_operations_lab_created (lab_id, created_at),
  CONSTRAINT fk_lab_operations_lab
    FOREIGN KEY (lab_id) REFERENCES lab_sessions (id) ON DELETE CASCADE,
  CONSTRAINT fk_lab_operations_user
    FOREIGN KEY (requested_by) REFERENCES users (id) ON DELETE RESTRICT,
  CONSTRAINT chk_lab_operations_status CHECK (
    status IN (
      'pending',
      'claimed',
      'running',
      'succeeded',
      'failed',
      'compensating'
    )
  ),
  CONSTRAINT chk_lab_operations_attempt_count
    CHECK (attempt_count >= 0)
) ENGINE=InnoDB;
