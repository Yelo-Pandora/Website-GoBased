USE platform;

CREATE TABLE IF NOT EXISTS orchestrator_lab_databases (
  database_name VARCHAR(64) NOT NULL,
  database_user VARCHAR(64) NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
    ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (database_name),
  UNIQUE KEY uk_orchestrator_lab_databases_user (database_user)
) ENGINE=InnoDB;

DROP PROCEDURE IF EXISTS provision_lab_database;
DROP PROCEDURE IF EXISTS destroy_lab_database;
DROP PROCEDURE IF EXISTS list_lab_databases;
DROP PROCEDURE IF EXISTS list_expected_lab_ids;

DELIMITER $$

CREATE PROCEDURE provision_lab_database(
  IN p_database_name VARCHAR(64),
  IN p_database_user VARCHAR(64),
  IN p_database_password VARCHAR(255)
)
SQL SECURITY DEFINER
BEGIN
  IF p_database_name NOT REGEXP '^lab_[a-z0-9]{4,32}$' THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'invalid lab database name';
  END IF;
  IF p_database_user NOT REGEXP '^lab_[a-z0-9]{4,32}_user$' THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'invalid lab database user';
  END IF;
  IF CHAR_LENGTH(p_database_password) < 16 THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'lab database password is too short';
  END IF;

  SET @sql_text = CONCAT(
    'CREATE DATABASE IF NOT EXISTS `',
    p_database_name,
    '` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci'
  );
  PREPARE statement_handle FROM @sql_text;
  EXECUTE statement_handle;
  DEALLOCATE PREPARE statement_handle;

  SET @sql_text = CONCAT(
    'CREATE USER IF NOT EXISTS ''',
    p_database_user,
    '''@''%'' IDENTIFIED BY ',
    QUOTE(p_database_password)
  );
  PREPARE statement_handle FROM @sql_text;
  EXECUTE statement_handle;
  DEALLOCATE PREPARE statement_handle;

  SET @sql_text = CONCAT(
    'ALTER USER ''',
    p_database_user,
    '''@''%'' IDENTIFIED BY ',
    QUOTE(p_database_password)
  );
  PREPARE statement_handle FROM @sql_text;
  EXECUTE statement_handle;
  DEALLOCATE PREPARE statement_handle;

  SET @sql_text = CONCAT(
    'GRANT SELECT, INSERT, UPDATE, DELETE ON `',
    p_database_name,
    '`.* TO ''',
    p_database_user,
    '''@''%'''
  );
  PREPARE statement_handle FROM @sql_text;
  EXECUTE statement_handle;
  DEALLOCATE PREPARE statement_handle;

  SET @sql_text = CONCAT(
    'CREATE TABLE IF NOT EXISTS `',
    p_database_name,
    '`.products (',
    'id BIGINT UNSIGNED NOT NULL,',
    'name VARCHAR(160) NOT NULL,',
    'category VARCHAR(64) NOT NULL,',
    'price DECIMAL(12,2) NOT NULL,',
    'currency CHAR(3) NOT NULL,',
    'stock_label VARCHAR(32) NOT NULL,',
    'description TEXT NOT NULL,',
    'version INT NOT NULL DEFAULT 1,',
    'updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ',
    'ON UPDATE CURRENT_TIMESTAMP(6),',
    'PRIMARY KEY (id),',
    'CONSTRAINT chk_products_price CHECK (price >= 0),',
    'CONSTRAINT chk_products_version CHECK (version > 0)',
    ') ENGINE=InnoDB'
  );
  PREPARE statement_handle FROM @sql_text;
  EXECUTE statement_handle;
  DEALLOCATE PREPARE statement_handle;

  SET @sql_text = CONCAT(
    'CREATE TABLE IF NOT EXISTS `',
    p_database_name,
    '`.order_stats (',
    'id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,',
    'product_id BIGINT UNSIGNED NOT NULL,',
    'instance_name VARCHAR(64) NOT NULL,',
    'time_bucket DATETIME(6) NOT NULL,',
    'received_orders INT NOT NULL DEFAULT 0,',
    'processed_orders INT NOT NULL DEFAULT 0,',
    'dropped_orders INT NOT NULL DEFAULT 0,',
    'updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ',
    'ON UPDATE CURRENT_TIMESTAMP(6),',
    'PRIMARY KEY (id),',
    'UNIQUE KEY uk_order_stats_bucket ',
    '(product_id, instance_name, time_bucket),',
    'CONSTRAINT fk_order_stats_product FOREIGN KEY (product_id) ',
    'REFERENCES products (id) ON DELETE RESTRICT,',
    'CONSTRAINT chk_order_stats_counts CHECK (',
    'received_orders >= 0 AND processed_orders >= 0 ',
    'AND dropped_orders >= 0)',
    ') ENGINE=InnoDB'
  );
  PREPARE statement_handle FROM @sql_text;
  EXECUTE statement_handle;
  DEALLOCATE PREPARE statement_handle;

  SET @sql_text = CONCAT(
    'INSERT INTO `',
    p_database_name,
    '`.products ',
    '(id, name, category, price, currency, stock_label, description, version) ',
    'VALUES ',
    '(1, ''Architecture Practice Laptop'', ''electronics'', 6999.00, ',
    '''CNY'', ''in_stock'', ''Product used by the application cluster lab.'', 1),',
    '(2, ''Distributed Systems Handbook'', ''books'', 129.00, ',
    '''CNY'', ''in_stock'', ''Product used by cache and database labs.'', 1) ',
    'ON DUPLICATE KEY UPDATE ',
    'name = VALUES(name), category = VALUES(category), price = VALUES(price), ',
    'currency = VALUES(currency), stock_label = VALUES(stock_label), ',
    'description = VALUES(description), version = VALUES(version)'
  );
  PREPARE statement_handle FROM @sql_text;
  EXECUTE statement_handle;
  DEALLOCATE PREPARE statement_handle;

  INSERT INTO platform.orchestrator_lab_databases (
    database_name,
    database_user
  ) VALUES (
    p_database_name,
    p_database_user
  ) ON DUPLICATE KEY UPDATE
    database_user = VALUES(database_user);
END$$

CREATE PROCEDURE destroy_lab_database(
  IN p_database_name VARCHAR(64),
  IN p_database_user VARCHAR(64)
)
SQL SECURITY DEFINER
BEGIN
  IF p_database_name NOT REGEXP '^lab_[a-z0-9]{4,32}$' THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'invalid lab database name';
  END IF;
  IF p_database_user NOT REGEXP '^lab_[a-z0-9]{4,32}_user$' THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'invalid lab database user';
  END IF;

  SET @sql_text = CONCAT('DROP DATABASE IF EXISTS `', p_database_name, '`');
  PREPARE statement_handle FROM @sql_text;
  EXECUTE statement_handle;
  DEALLOCATE PREPARE statement_handle;

  SET @sql_text = CONCAT(
    'DROP USER IF EXISTS ''',
    p_database_user,
    '''@''%'''
  );
  PREPARE statement_handle FROM @sql_text;
  EXECUTE statement_handle;
  DEALLOCATE PREPARE statement_handle;

  DELETE FROM platform.orchestrator_lab_databases
  WHERE database_name = p_database_name
    AND database_user = p_database_user;
END$$

CREATE PROCEDURE list_lab_databases()
SQL SECURITY DEFINER
READS SQL DATA
BEGIN
  SELECT managed.database_name
  FROM platform.orchestrator_lab_databases AS managed
  INNER JOIN information_schema.schemata AS available_schemas
    ON available_schemas.schema_name = managed.database_name
  ORDER BY managed.database_name;
END$$

CREATE PROCEDURE list_expected_lab_ids()
SQL SECURITY DEFINER
READS SQL DATA
BEGIN
  SELECT id
  FROM platform.lab_sessions
  WHERE status IN ('Preparing', 'Running', 'Expiring', 'Terminating')
  ORDER BY id;
END$$

DELIMITER ;
