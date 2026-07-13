USE platform;

DROP PROCEDURE IF EXISTS seed_bootstrap_users;

DELIMITER $$

CREATE PROCEDURE seed_bootstrap_users()
BEGIN
  DECLARE should_seed VARCHAR(16) DEFAULT 'false';

  SELECT LOWER(setting_value)
  INTO should_seed
  FROM bootstrap_settings
  WHERE setting_key = 'seed_test_users'
  LIMIT 1;

  IF should_seed = 'true' THEN
    INSERT INTO users (
      username,
      password_hash,
      status
    ) VALUES (
      'learner',
      '$2b$12$WmBVqMXazj6zMhauomUF0.HtLO3A0m4YeD.NDpgPSR5.H92kB/Ccy',
      'active'
    ) ON DUPLICATE KEY UPDATE
      password_hash = VALUES(password_hash),
      status = VALUES(status);
  END IF;
END$$

DELIMITER ;

CALL seed_bootstrap_users();
DROP PROCEDURE seed_bootstrap_users;
DROP TABLE bootstrap_settings;
