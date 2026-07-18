USE platform;

DROP PROCEDURE IF EXISTS seed_cache_lab_products;

DELIMITER $$

CREATE PROCEDURE seed_cache_lab_products(
  IN p_database_name VARCHAR(64)
)
SQL SECURITY DEFINER
BEGIN
  IF p_database_name NOT REGEXP '^lab_[a-z0-9]{4,32}$' THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'invalid lab database name';
  END IF;

  SET @sql_text = CONCAT('DELETE FROM `', p_database_name, '`.order_stats');
  PREPARE statement_handle FROM @sql_text;
  EXECUTE statement_handle;
  DEALLOCATE PREPARE statement_handle;

  SET @sql_text = CONCAT('DELETE FROM `', p_database_name, '`.products');
  PREPARE statement_handle FROM @sql_text;
  EXECUTE statement_handle;
  DEALLOCATE PREPARE statement_handle;

  SET @sql_text = CONCAT(
    'INSERT INTO `', p_database_name, '`.products ',
    '(id, name, category, price, currency, stock_label, description, version) VALUES ',
    '(1,''Architecture Practice Laptop'',''electronics'',6999,''CNY'',''in_stock'',''Cache lab product.'',1),',
    '(2,''Edge Wireless Router'',''electronics'',899,''CNY'',''in_stock'',''Cache lab product.'',1),',
    '(3,''Mechanical Lab Keyboard'',''electronics'',499,''CNY'',''in_stock'',''Cache lab product.'',1),',
    '(4,''Distributed Systems Handbook'',''books'',129,''CNY'',''in_stock'',''Cache lab product.'',1),',
    '(5,''Database Internals Guide'',''books'',139,''CNY'',''in_stock'',''Cache lab product.'',1),',
    '(6,''Go Concurrency Workbook'',''books'',99,''CNY'',''in_stock'',''Cache lab product.'',1),',
    '(7,''Smart Desk Lamp'',''home'',259,''CNY'',''in_stock'',''Cache lab product.'',1),',
    '(8,''Room Temperature Sensor'',''home'',169,''CNY'',''in_stock'',''Cache lab product.'',1),',
    '(9,''Air Quality Monitor'',''home'',399,''CNY'',''in_stock'',''Cache lab product.'',1),',
    '(10,''Trail Running Shoes'',''sports'',699,''CNY'',''in_stock'',''Cache lab product.'',1),',
    '(11,''Carbon Badminton Racket'',''sports'',599,''CNY'',''in_stock'',''Cache lab product.'',1),',
    '(12,''Fitness Activity Tracker'',''sports'',329,''CNY'',''in_stock'',''Cache lab product.'',1)'
  );
  PREPARE statement_handle FROM @sql_text;
  EXECUTE statement_handle;
  DEALLOCATE PREPARE statement_handle;
END$$

DELIMITER ;
