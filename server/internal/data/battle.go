package data

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// 战斗相关的原版数据（gmsv/char/encount.c、gmsv/char/enemy.c、gmsv/map/readmap.c 的读取规则）：
//   battle/encount.txt   遇敌区域
//   battle/group1.txt    敌人队伍
//   battle/enemy1.txt    敌人（引用 enemybase 的模板）
//   battle/enemybase1.txt 敌人模板（成长、属性、形象）
//   battle/exp.txt       人物升级经验表（setup.cf USEREXP，_USER_EXP_CF）
//   battle/petskill2.txt 宠物与敌人的技能（服务端实际读的那份）
//   map/battlemap.txt    地块 → 战斗背景编号
// setup.cf 里用的是 enemy1/group1/enemybase1 这一套。

// EncountArea 是 encount.txt 的一行：楼层上的一个矩形区域及其遇敌参数（ENCOUNT_Table）。
type EncountArea struct {
	Index, Floor       int
	X1, Y1, X2, Y2     int // 闭区间（原版 CoordinateInRect 用 x..x+width）
	ProbMin, ProbMax   int // 每步遇敌机率的下限与上限
	EnemyMax           int // 一场战斗最多出现的敌人数
	ZOrder             int // 区域重叠时取较大者；0 的区域不生效
	Groups, GroupProbs []int
}

// Group 是 group1.txt 的一行：一支敌人队伍可能出现的敌人与权重。
type Group struct {
	ID         int
	Name       string
	Enemies    []int // 敌人 ID
	Probs      []int // 对应权重
	AppearItem int   // 带着这件道具才出现（-1 不限）
	HideItem   int   // 带着这件道具就不出现（-1 不限）
}

// Enemy 是 enemy1.txt 的一行（ENEMY_DATAINT / ENEMY_DATACHAR）。
type Enemy struct {
	ID, TempNo   int
	Name         string
	Tactics      string // 行动方针，如 at:10;1;1|gu:1|es:1（battle_ai.c）
	LvMin, LvMax int
	CreateMax    int // 同种最多出现几只
	CreateMin    int
	TacticsMode  int
	Exp          int // -1 表示按等级查表（ENEMY_getExp）
	Style        int
	PetFlag      int
	Items        [10]int // 掉落道具（ENEMY_ITEM1..10）
	ItemProbs    [10]int // 掉落机率，千分比（_FIX_ITEMPROB：RAND(0,999) < 机率）
}

// EnemyTemp 是 enemybase1.txt 的一行（ENEMYTEMP_DATAINT）。
type EnemyTemp struct {
	TempNo     int
	Name       string
	InitNum    int    // 初始成长次数
	LvUpPoint  int    // 每级成长次数（原版 atoi，4.50 读作 4）
	Base       [4]int // 体力、腕力、耐力、敏捷的成长率
	Get        int    // 捕获难度（E_T_GET，捕获机率的基础值）
	ModAI      int    // 忠诚度修正（E_T_MODAI，算宠物忠诚度的分母）
	Slot       int    // 宠物的技能栏数（E_T_SLOT）
	LimitLevel int    // 宠物的等级上限（E_T_LIMITLEVEL，0 为不限）
	Attr       [4]int // 地水火风 0..100
	Status     [6]int // 毒、麻痹、睡眠、石化、酒醉、混乱抗性
	Skills     [7]int // 宠物技能栏（petskill 编号，-1 为空），敌人按行动方针的 wa 权重选用
	Rare       int
	Critical   int
	Counter    int
	Image      int // 战斗形象（动画编号）
	Size       int // 0 普通，1 大型
	// AtomFix 是这种宠物加工 / 料理时对某些素材的修正（E_T_ATOMFIXNAME1..5、ATOMBASEADD、ATOMFIXMIN、ATOMFIXMAX，ITEM_merge_getPetFix）
	AtomFix []AtomFix
}

// AtomFix 是宠物对一种素材的修正：数值加 Base，随机范围的下限、上限（千分比）是 Min、Max。
type AtomFix struct {
	Name           string
	Base, Min, Max int
}

// BattleData 汇总战斗用的原版数据。
type BattleData struct {
	Areas     []EncountArea
	Groups    map[int]*Group
	Enemies   map[int]*Enemy
	Temps     map[int]*EnemyTemp
	LevelExp  []int          // exp.txt 逐行（第 n 行在 [n-1]）：原版 NeedLevelUpTbls[n] = 第 n 行，n 级升 n+1 级要 tbl[n+1] = LevelExp[n]；每级经验从 0 重新累计（CHAR_HandleExp）
	BattleMap map[int][3]int // 地块图号 → 三个可选的战斗背景编号
	Skills    map[int]*Skill // petskill2.txt
	Magics    map[int]*Magic // magic.txt（咒术）
	AttMagics []AttMagic     // attmagic.bin（攻击魔法的范围）
}

func readLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range strings.Split(GBK(b), "\n") {
		l = strings.TrimSpace(strings.ReplaceAll(strings.TrimRight(l, "\r"), "\t", " "))
		if l == "" || l[0] == '#' {
			continue
		}
		out = append(out, l)
	}
	return out, nil
}

// cols 把一行按逗号拆开；原版 getStringFromIndexWithDelim 把空字段当作空串。
func cols(line string) []string { return strings.Split(line, ",") }

// num 是原版 atoi：取开头的整数部分，空串或非数字为 def。
func num(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	end := 0
	for end < len(s) && (s[end] >= '0' && s[end] <= '9' || (end == 0 && (s[end] == '-' || s[end] == '+'))) {
		end++
	}
	v, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0
	}
	return v
}

// LoadBattle 读取战斗数据；dataDir 是 gamedata 目录。
func LoadBattle(dataDir string) (*BattleData, error) {
	d := &BattleData{Groups: map[int]*Group{}, Enemies: map[int]*Enemy{}, Temps: map[int]*EnemyTemp{}, BattleMap: map[int][3]int{}}
	dir := filepath.Join(dataDir, "battle")

	// enemybase1.txt：前 6 个字段是名字和 5 个素材名，之后是 ENEMYTEMP 整数（原版不足的行整行丢弃，这里取到形象为止即可）
	lines, err := readLines(filepath.Join(dir, "enemybase1.txt"))
	if err != nil {
		return nil, err
	}
	for _, l := range lines {
		c := cols(l)
		if len(c) < 6+33 {
			continue
		}
		v := func(i int) int { return num(c[6+i], -1) }
		t := &EnemyTemp{TempNo: v(0), Name: c[0], InitNum: v(1), LvUpPoint: v(2),
			Base: [4]int{v(3), v(4), v(5), v(6)}, ModAI: v(7), Get: v(8), Slot: v(29), Attr: [4]int{v(9), v(10), v(11), v(12)},
			Rare: v(26), Critical: v(27), Counter: v(28), Image: v(30), Size: v(32)}
		for i := range t.Status {
			t.Status[i] = v(13 + i)
		}
		for i := range t.Skills {
			t.Skills[i] = v(19 + i)
		}
		for i := range t.Attr {
			t.Attr[i] = max(0, t.Attr[i])
		}
		if 6+48 < len(c) {
			t.LimitLevel = max(0, v(48))
		}
		for i := 0; i < 5; i++ { // 素材名在第 2..6 栏，数值在 ENEMYTEMP 整数的 33..47（空的当 0，同 atoi）
			if name := strings.TrimSpace(c[1+i]); name != "" && 6+35+i*3 < len(c) {
				n := func(k int) int { return num(c[6+33+i*3+k], 0) }
				t.AtomFix = append(t.AtomFix, AtomFix{name, n(0), n(1), n(2)})
			}
		}
		d.Temps[t.TempNo] = t
	}

	// enemy1.txt：名字、行动方针、行动条件（_BATTLENPC_WARP_PLAYER），之后是 ENEMY 整数
	if lines, err = readLines(filepath.Join(dir, "enemy1.txt")); err != nil {
		return nil, err
	}
	for _, l := range lines {
		c := cols(l)
		if len(c) < 3+11 {
			continue
		}
		v := func(i int) int { return num(c[3+i], 0) }
		e := &Enemy{Name: c[0], Tactics: c[1], ID: v(0), TempNo: v(1), LvMin: v(2), LvMax: v(3),
			CreateMax: v(4), CreateMin: v(5), TacticsMode: v(6), Exp: v(7), Style: v(9), PetFlag: v(10)}
		for i := 0; i < 10 && 3+21+i < len(c); i++ {
			e.Items[i] = num(c[3+11+i], 0)
			e.ItemProbs[i] = num(c[3+21+i], 0)
		}
		if d.Temps[e.TempNo] == nil {
			continue // 原版：模板不存在的敌人不读
		}
		if e.LvMin == 0 {
			e.LvMin = e.LvMax
		}
		e.LvMin, e.LvMax = min(e.LvMin, e.LvMax), max(e.LvMin, e.LvMax)
		d.Enemies[e.ID] = e
	}

	// group1.txt：名字，ID，出现/不出现道具，10 个敌人 ID，10 个权重；一个敌人都找不到的队伍丢弃
	if lines, err = readLines(filepath.Join(dir, "group1.txt")); err != nil {
		return nil, err
	}
	for _, l := range lines {
		c := cols(l)
		if len(c) < 1+23 {
			continue
		}
		v := func(i int) int { return num(c[1+i], -1) }
		g := &Group{Name: c[0], ID: v(0), AppearItem: v(1), HideItem: v(2)}
		for i := 0; i < 10; i++ {
			id, p := v(3+i), v(13+i)
			if id == -1 || d.Enemies[id] == nil {
				continue
			}
			g.Enemies = append(g.Enemies, id)
			g.Probs = append(g.Probs, max(0, p))
		}
		if len(g.Enemies) > 0 {
			d.Groups[g.ID] = g
		}
	}

	// encount.txt：编号,楼层,x1,y1,x2,y2,机率下限,机率上限,敌人上限,优先度,队伍×10,权重×10
	if lines, err = readLines(filepath.Join(dir, "encount.txt")); err != nil {
		return nil, err
	}
	for _, l := range lines {
		c := cols(l)
		if len(c) < 10 {
			continue
		}
		v := func(i int, def int) int {
			if i >= len(c) {
				return def
			}
			return num(c[i], def)
		}
		a := EncountArea{Index: v(0, -1), Floor: v(1, 0),
			X1: min(v(2, 0), v(4, 0)), X2: max(v(2, 0), v(4, 0)), Y1: min(v(3, 0), v(5, 0)), Y2: max(v(3, 0), v(5, 0)),
			ProbMin: min(v(6, 1), v(7, 50)), ProbMax: max(v(6, 1), v(7, 50)), EnemyMax: v(8, 4), ZOrder: v(9, 0)}
		if a.EnemyMax < 1 || a.EnemyMax > 10 {
			a.EnemyMax = 4
		}
		for i := 0; i < 10; i++ {
			gid, p := v(10+i, -1), v(20+i, -1)
			if gid == -1 {
				continue
			}
			a.Groups = append(a.Groups, gid)
			a.GroupProbs = append(a.GroupProbs, max(0, p))
		}
		d.Areas = append(d.Areas, a)
	}

	// exp.txt：每行"等级 经验"，第 n 个有效行就是 NeedLevelUpTbls[n]（configfile.c）
	if lines, err = readLines(filepath.Join(dir, "exp.txt")); err != nil {
		return nil, err
	}
	for _, l := range lines {
		if i := strings.IndexByte(l, '#'); i >= 0 {
			l = l[:i]
		}
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		d.LevelExp = append(d.LevelExp, num(f[1], 0))
	}

	if d.Skills, err = loadSkills(filepath.Join(dir, "petskill2.txt")); err != nil {
		return nil, err
	}
	d.Magics, d.AttMagics = LoadMagic(dataDir)
	if err := d.loadBattleMap(filepath.Join(dataDir, "map", "battlemap.txt")); err != nil {
		return nil, err
	}
	return d, nil
}

// loadBattleMap：$ a b c 设定当前的三个背景编号（只给一个时三个相同），之后的"n"或"n to m"行把这些地块设成当前背景。
// 同一地块重复设定时原版报错并以后者为准。
func (d *BattleData) loadBattleMap(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	cur := [3]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || l[0] == '#' {
			continue
		}
		if l[0] == '$' {
			var n [3]int
			k, _ := fmt.Sscanf(l[1:], "%d %d %d", &n[0], &n[1], &n[2])
			if k < 1 {
				continue
			}
			cur = [3]int{n[0], n[0], n[0]}
			for i := 0; i < k; i++ {
				cur[i] = n[i]
			}
			continue
		}
		var first int
		if _, err := fmt.Sscanf(l, "%d", &first); err != nil || first < 0 {
			continue
		}
		last := first
		if i := strings.Index(l, "to"); i >= 0 {
			if _, err := fmt.Sscanf(l[i+2:], "%d", &last); err != nil {
				last = first
			}
		}
		for t := first; t <= last; t++ {
			d.BattleMap[t] = cur
		}
	}
	return sc.Err()
}

// AreaAt 按 ENCOUNT_getEncountAreaArray 找 (floor,x,y) 所在、优先度最高的遇敌区域。
func (d *BattleData) AreaAt(floor, x, y int) *EncountArea {
	var best *EncountArea
	for i := range d.Areas {
		a := &d.Areas[i]
		if a.Floor != floor || a.ZOrder <= 0 || x < a.X1 || x > a.X2 || y < a.Y1 || y > a.Y2 {
			continue
		}
		if best == nil || a.ZOrder > best.ZOrder {
			best = a
		}
	}
	return best
}

// NextLevelExp 返回 level 级升下一级所需的经验（CHAR_GetLevelExp(level+1) = NeedLevelUpTbls[level+1]，即 exp.txt 第 level+1 行），
// 这是本级从 0 累计的量（升级时扣掉，CHAR_HandleExp _NEWOPEN_MAXEXP）；满级返回 -1。
func (d *BattleData) NextLevelExp(level int) int {
	if level >= len(d.LevelExp) || level < 0 {
		return -1
	}
	return d.LevelExp[level]
}

// enemyBaseExp 是 gmsv/include/enemyexptbl.h 的 enemybaseexptbl（按等级 1 起）。
var enemyBaseExp = []int{
	1, 2, 3, 4, 5, 6, 9, 12, 15, 18, 22, 26, 30, 35, 40, 46, 52, 58, 65, 72,
	79, 87, 95, 104, 113, 122, 131, 141, 151, 162, 173, 184, 196, 208, 220, 233, 246, 260, 274, 288,
	303, 318, 333, 348, 365, 381, 398, 415, 432, 450, 468, 486, 506, 525, 545, 564, 585, 606, 627, 648,
	670, 692, 714, 737, 760, 784, 808, 832, 857, 882, 907, 933, 959, 956, 1012, 1040, 1067, 1095, 1123, 1152,
	1181, 1210, 1240, 1270, 1300, 1331, 1362, 1394, 1426, 1458, 1490, 1524, 1557, 1590, 1625, 1659, 1694, 1729, 1764, 1800,
}

// EnemyRank 是 ENEMY_getRank：四项成长率之和对应的档次 0..5。
func EnemyRank(t *EnemyTemp) int {
	sum := t.Base[0] + t.Base[1] + t.Base[2] + t.Base[3]
	for i, n := range []int{100, 95, 90, 85, 80, 0} {
		if sum >= n {
			return i
		}
	}
	return 5
}

// EnemyExp 是 ENEMY_getExp：基础经验 + (档次加成 + 特性加成) × 等级。
func EnemyExp(e *Enemy, t *EnemyTemp, level int) int {
	if e.Exp != -1 {
		return e.Exp
	}
	if level < 1 || level > len(enemyBaseExp) {
		return 0
	}
	rank := []float64{2.5, 2.0, 1.5, 1.0, 0.5, 0.0}[EnemyRank(t)]
	pos := func(v int) int { return max(0, v) }
	alpha := float64(pos(t.Critical)+pos(t.Counter)+pos(t.Get)+pos(t.Status[0])+pos(t.Status[1])+
		pos(t.Status[2])+pos(t.Status[3])+pos(t.Status[4])+pos(t.Status[5]))/100.0 + float64(pos(t.Rare))
	ret := int(float64(enemyBaseExp[level-1]) + (rank+alpha)*float64(level))
	return max(1, ret)
}

// Skill 是 petskill.txt 的一行（PETSKILL_DATACHAR / DATAINT，_CFREE_petskill + _PETSKILL2_TXT）：
// 名字、说明、函数名、参数、条件、种类码，之后是编号、使用场合、目标、USETYPE、价格、ILLEGAL。
// _PETSKILL2_TXT 开着时 configfile.c 只登记 petskillfile2 这个键，setup.cf 里的 petskillfile1 不认，所以读的是 petskill2.txt。
type Skill struct {
	ID      int
	Name    string
	Func    string // 如 PETSKILL_ContinuationAttack
	Option  string // 如 "2"、"毒 turn 3  攻%-30"
	Comment string // 说明（宠物窗口与宠物技能店显示）
	Field   int    // PETSKILL_FIELD：0 都能用、1 只在战斗、2 只在地图
	Target  int    // PETSKILL_TARGET：0 自己、1 别人（含自己）、2 我方全体、3 对方全体、4 全部、5 无、6 别人（不含自己）、7 自己与宠物以外
	Price   int    // PETSKILL_COST：宠物技能店的价格
}

func loadSkills(path string) (map[int]*Skill, error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}
	out := map[int]*Skill{}
	for _, l := range lines {
		c := cols(l)
		if len(c) < 7 {
			continue
		}
		s := &Skill{ID: num(c[6], -1), Name: c[0], Func: strings.TrimSpace(c[2]), Option: c[3], Comment: c[1]}
		if len(c) >= 11 {
			s.Field, s.Target, s.Price = num(c[7], 0), num(c[8], 0), num(c[10], 0)
		}
		if s.ID >= 0 {
			out[s.ID] = s
		}
	}
	return out, nil
}
