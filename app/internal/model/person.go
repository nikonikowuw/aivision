package model

// Person 人员基础信息模型。
// 内部主键自增 uint64；person_id 为对外唯一标识（创建后不可修改，且在所有记录含软删除中全局唯一）。
type Person struct {
	BaseModel
	PersonID string `gorm:"column:person_id;size:64;not null;uniqueIndex:uk_persons_person_id" json:"personId"`
	Name     string `gorm:"column:name;size:64;not null" json:"name"`
}

// TableName 返回人员表名。
func (Person) TableName() string {
	return "persons"
}
