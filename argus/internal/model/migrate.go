package model

import "gorm.io/gorm"

// AutoMigrate 建/升级全部 12 张表；无 FK，纯逻辑关联。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&User{},
		&Role{},
		&Menu{},
		&Department{},
		&UserRole{},
		&RoleMenu{},
		&RefreshToken{},
		&OperationLog{},
		&SystemConfig{},
		&Camera{},
		&Person{},
		&Algorithm{},
		&AlgorithmVersion{},
		&AnalysisTask{},
		&AlgorithmInstance{},
		&DesiredStateRevision{},
		&AlarmRecord{},
	)
}
