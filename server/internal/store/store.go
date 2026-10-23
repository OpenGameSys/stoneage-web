// Package store 持久化账号与角色。M1 用 SQLite（modernc.org/sqlite，纯 Go，无需 cgo）；
// 对外只暴露 Store 接口，换 MySQL/PostgreSQL 时另写一个实现即可。
package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrExists   = errors.New("已存在")
	ErrNotFound = errors.New("不存在")
	ErrBadLogin = errors.New("账号或密码错误")
	ErrMailFull = errors.New("信箱满了")
	ErrNoSecret = errors.New("这个账号没有设定安全码")
)

type Account struct {
	ID   int64
	Name string
}

// Character 是存档中的角色。Dir 为客户端方向。
type Character struct {
	ID        int64
	AccountID int64
	Name      string
	Sprite    int
	Level     int
	Hometown  int
	Floor     int
	X, Y, Dir int
	// 以下为原版建角参数：头像、基本状态点数、四属性（0..10）、魅力、登录次数
	Face                     int
	Vital, Str, Tgh, Dex     int
	Earth, Water, Fire, Wind int
	Charm, Logins            int
	// 状态窗口用（原版 pc.exp / pc.dp / pc.transmigration）
	Exp, DP, Trans int
	// 当前 HP（-1 表示满）、MP、未分配的升级点数（原版 CHAR_SKILLUPPOINT）
	HP, MP, Points int
	// 金钱与道具（道具是世界层序列化的 JSON：9 个装备位 + 45 格道具栏）
	Gold  int
	Items string
	// 记录点（原版 CHAR_LASTTALKELDER，-1 表示出生村落的长老）与已登记过的记录点（CHAR_SAVEPOINT 的位元，第 n 位是长老编号 n）
	Elder      int
	SavePoints int64
	// 个人声望（原版 CHAR_FAME，内部是"点数 × 100"）与寄放在寄放道具店的道具（JSON，最多 30 件）
	Fame int
	Pool string
	// 任务旗标（原版 CHAR_ENDEVENT..10 / CHAR_NOWEVENT..10 的位元）：做完的、进行中的事件编号，以逗号分开
	EndEvents, NowEvents string
	// 宠物（世界层序列化的 JSON：宠物栏 5 格与出战的宠物，原版 CHAR_MAXPETHAVE / CHAR_DEFAULTPET）
	Pets string
	// 魔法熟练度与抗魔（世界层序列化的 JSON：地水火风各自的熟练度、抗魔、两种经验，原版 CHAR_EARTH_EXP.. 等）
	Magic string
	// 提升学习经验的能力（ITEM_Addexp：CHAR_ADDEXPPOWER, CHAR_ADDEXPTIME）："百分比,剩下的秒数"
	AddExp string
	// 乌力化剩下的秒数（CHAR_BECOMEPIG，-1 / 0 为没有）
	Pig int
	// 名片匣（世界层序列化的 JSON：ADDRESSBOOK_MAX 80 格，每格是对方的角色编号与名字、等级、DP、头像、转生）
	AddressBook string
	// 家族（世界层序列化的 JSON：CHAR_FMINDEX、职位、守护精灵、个人气势、家族银行个人帐户等；家族本身的资料在 families 表）
	Family string
	// 运气 1..5 与抽运气的时间（CHAR_LUCK / CHAR_LASTTIMESETLUCK，Unix 秒；0 是还没抽过）
	Luck     int
	LuckTime int64
}

type Store interface {
	// 账号：password / secret 都是客户端算好的 MD5（32 位十六进制），服务端照存、照比对；secret 是注册时填的安全码，重设密码用
	CreateAccount(ctx context.Context, name, password, secret string) (Account, error)
	Authenticate(ctx context.Context, name, password string) (Account, error)
	// ResetPassword 用安全码重设密码：账号不存在或安全码不对回 ErrBadLogin，账号没有安全码（改版前注册的）回 ErrNoSecret
	ResetPassword(ctx context.Context, name, secret, password string) error
	// SetPassword / SetSecret 是 GM 替账号改密码 / 安全码（值同样是 MD5）；账号不存在回 ErrBadLogin
	SetPassword(ctx context.Context, name, password string) error
	SetSecret(ctx context.Context, name, secret string) error
	// AccountInfo 是系统窗口"个人信息"要的：账号名、注册时间、有没有设安全码
	AccountInfo(ctx context.Context, id int64) (AccountInfo, error)
	AccountExists(ctx context.Context, name string) (bool, error)
	ListCharacters(ctx context.Context, accountID int64) ([]Character, error)
	CreateCharacter(ctx context.Context, c Character) (Character, error)
	DeleteCharacter(ctx context.Context, accountID, charID int64) error
	GetCharacter(ctx context.Context, accountID, charID int64) (Character, error)
	SavePosition(ctx context.Context, charID int64, floor, x, y, dir int) error
	AddLogin(ctx context.Context, charID int64) error    // 登录次数 +1（角色选择界面显示）
	SaveProgress(ctx context.Context, c Character) error // 战斗、升级、加点、回复之后保存等级与各项数值
	// 传言板（npc_dengon.c 的 ./Dengon/<楼层>_<x>_<y> 档）：每块板的留言按编号从 1 递增，只留最新的 DengonKeep 则
	DengonMaxID(ctx context.Context, board string) (int, error)
	DengonPosts(ctx context.Context, board string, fromID, toID int) (map[int]string, error)
	AddDengon(ctx context.Context, board, text string) (int, error)
	// 寄给不在线的人的邮件（原版经 AC 服务器转送）：上线时取出并删掉
	// 强者表（原版在 AC 的 db_duel）：按 DP 由高到低，同分同名次
	DuelRanking(ctx context.Context, start, n int) ([]DuelRow, error)
	DuelRank(ctx context.Context, charID int64) (int, error) // 名次（0 起）
	// 家族（原版在 AC 的 data/family）：每个家族一笔 JSON，世界开机时全部读进来
	LoadFamilies(ctx context.Context) (map[int]string, error)
	SaveFamily(ctx context.Context, id int, data string) error
	DeleteFamily(ctx context.Context, id int) error
	// 其他整个世界共用的资料（庄园表、家族之间的留言板等）
	GetKV(ctx context.Context, key string) (string, error)
	// AllCharacters 列出所有没删除的角色（开机时换算旧存档用）；SaveExpPets 只写回经验与宠物栏
	AllCharacters(ctx context.Context) ([]Character, error)
	SaveExpPets(ctx context.Context, charID int64, exp int, pets string) error
	PutKV(ctx context.Context, key, value string) error
	PutMail(ctx context.Context, to int64, body string) error
	TakeMails(ctx context.Context, to int64) ([]string, error)
	Close() error
}

// DengonKeep 是 DENGONFILELINENUM：每块传言板保留的留言数（档案是 1000 行的环）。
const DengonKeep = 1000

type sqliteStore struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS accounts (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE COLLATE NOCASE,
	pass_hash BLOB NOT NULL,
	pass_salt BLOB NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS characters (
	id INTEGER PRIMARY KEY,
	account_id INTEGER NOT NULL REFERENCES accounts(id),
	name TEXT NOT NULL UNIQUE,
	sprite INTEGER NOT NULL,
	level INTEGER NOT NULL DEFAULT 1,
	hometown INTEGER NOT NULL,
	floor INTEGER NOT NULL,
	x INTEGER NOT NULL,
	y INTEGER NOT NULL,
	dir INTEGER NOT NULL DEFAULT 0,
	deleted INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS characters_account ON characters(account_id, deleted);
CREATE TABLE IF NOT EXISTS dengon (
	board TEXT NOT NULL,
	id INTEGER NOT NULL,
	text TEXT NOT NULL,
	PRIMARY KEY (board, id)
);
`

// 增量加列：旧库缺列时补上（已存在则忽略报错）
var migrations = []string{
	`ALTER TABLE characters ADD COLUMN face INTEGER NOT NULL DEFAULT 30000`,
	`ALTER TABLE characters ADD COLUMN vital INTEGER NOT NULL DEFAULT 10`,
	`ALTER TABLE characters ADD COLUMN str INTEGER NOT NULL DEFAULT 10`,
	`ALTER TABLE characters ADD COLUMN tgh INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN dex INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN earth INTEGER NOT NULL DEFAULT 10`,
	`ALTER TABLE characters ADD COLUMN water INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN fire INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN wind INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN charm INTEGER NOT NULL DEFAULT 60`,
	`ALTER TABLE characters ADD COLUMN logins INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN exp INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN dp INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN trans INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN hp INTEGER NOT NULL DEFAULT -1`,
	`ALTER TABLE characters ADD COLUMN mp INTEGER NOT NULL DEFAULT 100`,
	`ALTER TABLE characters ADD COLUMN points INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN gold INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN items TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE characters ADD COLUMN elder INTEGER NOT NULL DEFAULT -1`,
	`ALTER TABLE characters ADD COLUMN savepoints INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN fame INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN pool TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE characters ADD COLUMN endevent TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE characters ADD COLUMN nowevent TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE characters ADD COLUMN pets TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE characters ADD COLUMN magic TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE characters ADD COLUMN addexp TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE characters ADD COLUMN pig INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN addressbook TEXT NOT NULL DEFAULT ''`,
	`CREATE TABLE IF NOT EXISTS mails (id INTEGER PRIMARY KEY, to_char INTEGER NOT NULL, body TEXT NOT NULL, created_at INTEGER NOT NULL)`,
	`ALTER TABLE characters ADD COLUMN family TEXT NOT NULL DEFAULT ''`,
	`CREATE TABLE IF NOT EXISTS families (id INTEGER PRIMARY KEY, data TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	`ALTER TABLE characters ADD COLUMN luck INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE characters ADD COLUMN luck_time INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE accounts ADD COLUMN secret BLOB NOT NULL DEFAULT X''`,
}

// OpenSQLite 打开（必要时创建）数据库文件。
func OpenSQLite(path string) (Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite 单写者；M1 的写入量很小
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("建表：%w", err)
	}
	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			db.Close()
			return nil, fmt.Errorf("升级表结构：%w", err)
		}
	}
	return &sqliteStore{db}, nil
}

// passwordKey 是存进 pass_hash 的内容：客户端已经把密码算成 MD5（32 位十六进制，session.go 检查格式），服务端照存、照比对，
// 不再做 PBKDF2（pass_salt 栏位留着但是空的）。大小写统一成小写，免得两边的十六进制写法不同。
func passwordKey(password string) []byte { return []byte(strings.ToLower(password)) }

func (s *sqliteStore) CreateAccount(ctx context.Context, name, password, secret string) (Account, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO accounts(name, pass_hash, pass_salt, secret, created_at) VALUES(?,?,?,?,?)`,
		name, passwordKey(password), []byte{}, passwordKey(secret), time.Now().Unix())
	if err != nil {
		if isUnique(err) {
			return Account{}, ErrExists
		}
		return Account{}, err
	}
	id, _ := res.LastInsertId()
	return Account{ID: id, Name: name}, nil
}

func (s *sqliteStore) ResetPassword(ctx context.Context, name, secret, password string) error {
	var id int64
	var stored []byte
	err := s.db.QueryRowContext(ctx, `SELECT id, secret FROM accounts WHERE name = ?`, name).Scan(&id, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBadLogin
	}
	if err != nil {
		return err
	}
	if len(stored) == 0 {
		return ErrNoSecret
	}
	if subtle.ConstantTimeCompare(stored, passwordKey(secret)) != 1 {
		return ErrBadLogin
	}
	_, err = s.db.ExecContext(ctx, `UPDATE accounts SET pass_hash = ?, pass_salt = X'' WHERE id = ?`, passwordKey(password), id)
	return err
}

// AccountInfo 是账号的基本资料。
type AccountInfo struct {
	Name      string
	CreatedAt time.Time
	HasSecret bool
}

func (s *sqliteStore) AccountInfo(ctx context.Context, id int64) (AccountInfo, error) {
	var a AccountInfo
	var created int64
	var secret []byte
	err := s.db.QueryRowContext(ctx, `SELECT name, created_at, secret FROM accounts WHERE id = ?`, id).Scan(&a.Name, &created, &secret)
	if err != nil {
		return a, err
	}
	a.CreatedAt = time.Unix(created, 0)
	a.HasSecret = len(secret) > 0
	return a, nil
}

func (s *sqliteStore) SetPassword(ctx context.Context, name, password string) error {
	return s.setAccountField(ctx, "pass_hash", name, passwordKey(password))
}

func (s *sqliteStore) SetSecret(ctx context.Context, name, secret string) error {
	return s.setAccountField(ctx, "secret", name, passwordKey(secret))
}

func (s *sqliteStore) setAccountField(ctx context.Context, col, name string, v []byte) error {
	res, err := s.db.ExecContext(ctx, `UPDATE accounts SET `+col+` = ? WHERE name = ?`, v, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrBadLogin
	}
	return nil
}

func (s *sqliteStore) AccountExists(ctx context.Context, name string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE name = ?`, name).Scan(&n)
	return n > 0, err
}

func (s *sqliteStore) Authenticate(ctx context.Context, name, password string) (Account, error) {

	var a Account
	var hash, salt []byte
	err := s.db.QueryRowContext(ctx, `SELECT id, name, pass_hash, pass_salt FROM accounts WHERE name = ?`, name).
		Scan(&a.ID, &a.Name, &hash, &salt)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrBadLogin
	}
	if err != nil {
		return Account{}, err
	}
	_ = salt // 旧版（PBKDF2）的账号：hash 是 32 字节的派生值、salt 非空，对不上 MD5，只能重新注册
	if subtle.ConstantTimeCompare(hash, passwordKey(password)) != 1 {
		return Account{}, ErrBadLogin
	}
	return a, nil
}

const charCols = `id, account_id, name, sprite, level, hometown, floor, x, y, dir,
	face, vital, str, tgh, dex, earth, water, fire, wind, charm, logins, exp, dp, trans, hp, mp, points, gold, items, elder, savepoints, fame, pool, endevent, nowevent, pets, magic, addexp, pig, addressbook, family, luck, luck_time`

func scanChar(row interface{ Scan(...any) error }) (Character, error) {
	var c Character
	err := row.Scan(&c.ID, &c.AccountID, &c.Name, &c.Sprite, &c.Level, &c.Hometown, &c.Floor, &c.X, &c.Y, &c.Dir,
		&c.Face, &c.Vital, &c.Str, &c.Tgh, &c.Dex, &c.Earth, &c.Water, &c.Fire, &c.Wind, &c.Charm, &c.Logins, &c.Exp, &c.DP, &c.Trans, &c.HP, &c.MP, &c.Points, &c.Gold, &c.Items, &c.Elder, &c.SavePoints, &c.Fame, &c.Pool, &c.EndEvents, &c.NowEvents, &c.Pets, &c.Magic, &c.AddExp, &c.Pig, &c.AddressBook, &c.Family, &c.Luck, &c.LuckTime)
	return c, err
}

func (s *sqliteStore) ListCharacters(ctx context.Context, accountID int64) ([]Character, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+charCols+` FROM characters WHERE account_id = ? AND deleted = 0 ORDER BY id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Character{}
	for rows.Next() {
		c, err := scanChar(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *sqliteStore) CreateCharacter(ctx context.Context, c Character) (Character, error) {
	if c.HP <= 0 {
		c.HP = -1 // 新角色满血（原版建角时 HP = 最大 HP，MP = 100）
	}
	if c.MP <= 0 {
		c.MP = 100
	}
	c.Elder = c.Hometown // CHAR_getInitElderPosition：新角色的记录点是出生村落的长老
	res, err := s.db.ExecContext(ctx, `INSERT INTO characters(account_id, name, sprite, level, hometown, floor, x, y, dir, created_at,
		face, vital, str, tgh, dex, earth, water, fire, wind, charm, logins, exp, dp, trans, hp, mp, points, gold, items, elder, savepoints, fame, pool, endevent, nowevent, pets, magic, addexp, pig, addressbook, family, luck, luck_time)
		VALUES(?,?,?,?,?,?,?,?,?,?, ?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, c.AccountID, c.Name, c.Sprite, c.Level, c.Hometown, c.Floor, c.X, c.Y, c.Dir, time.Now().Unix(),
		c.Face, c.Vital, c.Str, c.Tgh, c.Dex, c.Earth, c.Water, c.Fire, c.Wind, c.Charm, c.Logins, c.Exp, c.DP, c.Trans, c.HP, c.MP, c.Points, c.Gold, c.Items, c.Elder, c.SavePoints, c.Fame, c.Pool, c.EndEvents, c.NowEvents, c.Pets, c.Magic, c.AddExp, c.Pig, c.AddressBook, c.Family, c.Luck, c.LuckTime)
	if err != nil {
		if isUnique(err) {
			return Character{}, ErrExists
		}
		return Character{}, err
	}
	c.ID, _ = res.LastInsertId()
	return c, nil
}

// DeleteCharacter 只做标记删除；名字释放出来给别人用。
func (s *sqliteStore) DeleteCharacter(ctx context.Context, accountID, charID int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE characters SET deleted = 1, name = name || '#' || id WHERE id = ? AND account_id = ? AND deleted = 0`, charID, accountID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqliteStore) GetCharacter(ctx context.Context, accountID, charID int64) (Character, error) {
	c, err := scanChar(s.db.QueryRowContext(ctx, `SELECT `+charCols+` FROM characters WHERE id = ? AND account_id = ? AND deleted = 0`, charID, accountID))
	if errors.Is(err, sql.ErrNoRows) {
		return Character{}, ErrNotFound
	}
	return c, err
}

func (s *sqliteStore) SavePosition(ctx context.Context, charID int64, floor, x, y, dir int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE characters SET floor = ?, x = ?, y = ?, dir = ? WHERE id = ?`, floor, x, y, dir, charID)
	return err
}

func (s *sqliteStore) AddLogin(ctx context.Context, charID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE characters SET logins = logins + 1 WHERE id = ?`, charID)
	return err
}

func (s *sqliteStore) AllCharacters(ctx context.Context) ([]Character, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+charCols+` FROM characters WHERE deleted = 0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Character{}
	for rows.Next() {
		c, err := scanChar(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *sqliteStore) SaveExpPets(ctx context.Context, charID int64, exp int, pets string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE characters SET exp=?, pets=? WHERE id = ?`, exp, pets, charID)
	return err
}

func (s *sqliteStore) SaveProgress(ctx context.Context, c Character) error {
	_, err := s.db.ExecContext(ctx, `UPDATE characters SET level=?, exp=?, dp=?, charm=?, hp=?, mp=?, points=?,
		vital=?, str=?, tgh=?, dex=?, earth=?, water=?, fire=?, wind=?, trans=?, gold=?, items=?, elder=?, savepoints=?, fame=?, pool=?, endevent=?, nowevent=?, pets=?, magic=?, addexp=?, pig=?, addressbook=?, family=?,
		luck=?, luck_time=? WHERE id = ?`,
		c.Level, c.Exp, c.DP, c.Charm, c.HP, c.MP, c.Points, c.Vital, c.Str, c.Tgh, c.Dex, c.Earth, c.Water, c.Fire, c.Wind, c.Trans,
		c.Gold, c.Items, c.Elder, c.SavePoints, c.Fame, c.Pool, c.EndEvents, c.NowEvents, c.Pets, c.Magic, c.AddExp, c.Pig, c.AddressBook, c.Family,
		c.Luck, c.LuckTime, c.ID)

	return err
}

func (s *sqliteStore) DengonMaxID(ctx context.Context, board string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM dengon WHERE board = ?`, board).Scan(&n)
	return n, err
}

func (s *sqliteStore) DengonPosts(ctx context.Context, board string, fromID, toID int) (map[int]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, text FROM dengon WHERE board = ? AND id BETWEEN ? AND ?`, board, fromID, toID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var id int
		var t string
		if err := rows.Scan(&id, &t); err != nil {
			return nil, err
		}
		out[id] = t
	}
	return out, rows.Err()
}

// AddDengon 写入下一个编号，并删掉超出 DengonKeep 的旧留言（原版环形档案会被覆盖）。单一连线，编号不会撞。
func (s *sqliteStore) AddDengon(ctx context.Context, board, text string) (int, error) {
	id, err := s.DengonMaxID(ctx, board)
	if err != nil {
		return 0, err
	}
	id++
	if _, err := s.db.ExecContext(ctx, `INSERT INTO dengon(board, id, text) VALUES(?,?,?)`, board, id, text); err != nil {
		return 0, err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM dengon WHERE board = ? AND id <= ?`, board, id-DengonKeep)
	return id, err
}

func (s *sqliteStore) Close() error { return s.db.Close() }

func isUnique(err error) bool { return strings.Contains(err.Error(), "UNIQUE") }

// MailMax 是一个收件人最多存几封离线邮件（原版的 AC 没有上限）；满了就不收，免得被灌爆。
const MailMax = 50

func (s *sqliteStore) PutMail(ctx context.Context, to int64, body string) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mails WHERE to_char = ?`, to).Scan(&n); err != nil {
		return err
	}
	if n >= MailMax {
		return ErrMailFull
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO mails(to_char, body, created_at) VALUES(?,?,?)`, to, body, time.Now().Unix())
	return err
}

// TakeMails 按寄出的顺序取出 to 的邮件并删掉；超过 MailKeep 的不要了。
func (s *sqliteStore) TakeMails(ctx context.Context, to int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT body FROM mails WHERE to_char = ? AND created_at >= ? ORDER BY id LIMIT ?`, to, time.Now().Add(-MailKeep).Unix(), MailMax)
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, b)
	}
	rows.Close()
	_, err = s.db.ExecContext(ctx, `DELETE FROM mails WHERE to_char = ?`, to)
	return out, err
}

// MailKeep 是邮件保留多久（原版 AC 的 MAIL_EXPIRE_TIME 只有 1 小时、上线补送又有错，这里照宠物邮件的 3 天）。
const MailKeep = 3 * 24 * time.Hour

// DuelRow 是强者表的一行。
type DuelRow struct {
	ID    int64
	Name  string
	Level int
	DP    int
	Rank  int // 0 起；DP 相同的名次相同（saac db.c：同分的用第一个的名次）
}

func (s *sqliteStore) DuelRanking(ctx context.Context, start, n int) ([]DuelRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.name, c.level, c.dp,
		(SELECT COUNT(*) FROM characters d WHERE d.deleted = 0 AND d.dp > c.dp)
		FROM characters c WHERE c.deleted = 0 ORDER BY c.dp DESC, c.id LIMIT ? OFFSET ?`, n, start)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DuelRow
	for rows.Next() {
		var r DuelRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Level, &r.DP, &r.Rank); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DuelRank(ctx context.Context, charID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM characters d WHERE d.deleted = 0 AND
		(d.dp > (SELECT dp FROM characters WHERE id = ?) OR d.dp = (SELECT dp FROM characters WHERE id = ?) AND d.id < ?)`,
		charID, charID, charID).Scan(&n)
	return n, err
}

func (s *sqliteStore) LoadFamilies(ctx context.Context) (map[int]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, data FROM families`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var id int
		var d string
		if err := rows.Scan(&id, &d); err != nil {
			return nil, err
		}
		out[id] = d
	}
	return out, rows.Err()
}

func (s *sqliteStore) SaveFamily(ctx context.Context, id int, data string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO families(id, data) VALUES(?, ?) ON CONFLICT(id) DO UPDATE SET data = excluded.data`, id, data)
	return err
}

func (s *sqliteStore) DeleteFamily(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM families WHERE id = ?`, id)
	return err
}

// GetKV 回传 key 的值，没有时回空字串。
func (s *sqliteStore) GetKV(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *sqliteStore) PutKV(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
