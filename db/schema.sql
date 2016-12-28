BEGIN TRANSACTION ;
	CREATE TABLE IF NOT EXISTS dongles(
		imei string,
		imsi string,
		path string,
		properties blob,
		created_on time,
		updated_on time);


		CREATE UNIQUE INDEX UQE_dongels on dongles(imei,imsi);
COMMIT;
