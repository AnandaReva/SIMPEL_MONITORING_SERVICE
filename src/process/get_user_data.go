package process

type UserData struct {
	UserId   int64 `db:"id"`
	Username string `db:"username"`
	Role     string `db:"role"`
}

func Get_User_Data() {

}
