DROP PROCEDURE IF EXISTS platform.list_lab_database_resources;

DELIMITER //

CREATE PROCEDURE platform.list_lab_database_resources()
SQL SECURITY DEFINER
READS SQL DATA
BEGIN
  SELECT
    s.SCHEMA_NAME AS database_name,
    CONCAT(s.SCHEMA_NAME, '_user') AS user_name
  FROM information_schema.SCHEMATA AS s
  WHERE s.SCHEMA_NAME REGEXP '^lab_[a-z0-9]{4,23}$'
  ORDER BY s.SCHEMA_NAME;
END//

DELIMITER ;
