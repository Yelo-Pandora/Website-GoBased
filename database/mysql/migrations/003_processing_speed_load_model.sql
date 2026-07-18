DROP PROCEDURE IF EXISTS platform.migrate_platform_load_model;
DROP PROCEDURE IF EXISTS platform.migrate_lab_order_stats;

DELIMITER //

CREATE PROCEDURE platform.migrate_platform_load_model()
SQL SECURITY DEFINER
MODIFIES SQL DATA
BEGIN
  IF EXISTS (
    SELECT 1
    FROM information_schema.TABLE_CONSTRAINTS
    WHERE CONSTRAINT_SCHEMA = 'platform'
      AND TABLE_NAME = 'lab_instances'
      AND CONSTRAINT_NAME = 'chk_lab_instances_capacity'
  ) THEN
    ALTER TABLE platform.lab_instances
      DROP CHECK chk_lab_instances_capacity;
  END IF;

  IF EXISTS (
    SELECT 1
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = 'platform'
      AND TABLE_NAME = 'lab_instances'
      AND COLUMN_NAME = 'effective_capacity'
  ) THEN
    ALTER TABLE platform.lab_instances
      CHANGE COLUMN effective_capacity processing_speed INT NOT NULL;
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = 'platform'
      AND TABLE_NAME = 'lab_instances'
      AND COLUMN_NAME = 'max_load'
  ) THEN
    ALTER TABLE platform.lab_instances
      ADD COLUMN max_load INT NOT NULL DEFAULT 100 AFTER processing_speed;
  END IF;

  UPDATE platform.lab_instances
  SET processing_speed = GREATEST(1, 20 * performance_percent DIV 100),
      max_load = 100;

  IF NOT EXISTS (
    SELECT 1
    FROM information_schema.TABLE_CONSTRAINTS
    WHERE CONSTRAINT_SCHEMA = 'platform'
      AND TABLE_NAME = 'lab_instances'
      AND CONSTRAINT_NAME = 'chk_lab_instances_processing_speed'
  ) THEN
    ALTER TABLE platform.lab_instances
      ADD CONSTRAINT chk_lab_instances_processing_speed
        CHECK (processing_speed > 0);
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM information_schema.TABLE_CONSTRAINTS
    WHERE CONSTRAINT_SCHEMA = 'platform'
      AND TABLE_NAME = 'lab_instances'
      AND CONSTRAINT_NAME = 'chk_lab_instances_max_load'
  ) THEN
    ALTER TABLE platform.lab_instances
      ADD CONSTRAINT chk_lab_instances_max_load
        CHECK (max_load > 0);
  END IF;
END//

CREATE PROCEDURE platform.migrate_lab_order_stats()
SQL SECURITY DEFINER
MODIFIES SQL DATA
BEGIN
  DECLARE finished INT DEFAULT 0;
  DECLARE database_name VARCHAR(64);
  DECLARE database_cursor CURSOR FOR
    SELECT TABLE_SCHEMA
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA REGEXP '^lab_[a-z0-9]{4,23}$'
      AND TABLE_NAME = 'order_stats'
      AND COLUMN_NAME = 'processed_orders';
  DECLARE CONTINUE HANDLER FOR NOT FOUND SET finished = 1;

  OPEN database_cursor;
  migration_loop: LOOP
    FETCH database_cursor INTO database_name;
    IF finished = 1 THEN
      LEAVE migration_loop;
    END IF;
    SET @sql_text = CONCAT(
      'ALTER TABLE `',
      database_name,
      '`.order_stats CHANGE COLUMN processed_orders accepted_orders ',
      'INT NOT NULL DEFAULT 0'
    );
    PREPARE statement_handle FROM @sql_text;
    EXECUTE statement_handle;
    DEALLOCATE PREPARE statement_handle;
  END LOOP;
  CLOSE database_cursor;
END//

DELIMITER ;

CALL platform.migrate_platform_load_model();
CALL platform.migrate_lab_order_stats();

DROP PROCEDURE platform.migrate_platform_load_model;
DROP PROCEDURE platform.migrate_lab_order_stats;
