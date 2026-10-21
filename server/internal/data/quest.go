package data

import (
	"os"
	"path/filepath"
	"strings"
)

// JobDaily 是任务日志的一行（gmsv configfile.c 读 data/jobdaily.txt，_JOBDAILY）：
// "任务编号|条件|任务名|状态"。条件的写法和 NPC 的 EVENT 条件一样（"," 或、"&" 且，NOWEV= / ENDEV= / !=），
// 成立的那几行就是玩家任务日志里要列的（CHAR_JobDaily）。同一个任务编号通常有"进行中"与"已完成"两行。
type JobDaily struct {
	ID    int
	Rule  string
	Name  string
	State string
	Hint  string // 第 5 栏（H5 补充的 jobdaily_h5.txt 才有）：提示文字，"npc:名字" 由 world 换成名字与楼层
}

// LoadJobDaily 读 jobdaily.txt（GBK，# 开头是注解，# 之后的也是）。
func LoadJobDaily(path string) ([]JobDaily, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []JobDaily
	for _, line := range strings.Split(GBK(b), "\n") {
		line = strings.TrimRight(line, "\r")
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		f := strings.Split(line, "|")
		if len(f) < 4 || strings.TrimSpace(f[0]) == "" {
			continue
		}
		j := JobDaily{ID: Atoi(strings.TrimSpace(f[0])), Rule: strings.TrimSpace(f[1]), Name: strings.TrimSpace(f[2]), State: strings.TrimSpace(f[3])}
		if len(f) > 4 {
			j.Hint = strings.TrimSpace(f[4])
		}
		out = append(out, j)
	}
	return out, nil
}

// loadJobDailyFile：原版的 jobdaily.txt 加上 H5 补充的 jobdaily_h5.txt（用道具记进度的任务，如 JOT）。
func loadJobDailyFile(dataDir string) []JobDaily {
	q, _ := LoadJobDaily(filepath.Join(dataDir, "jobdaily.txt"))
	extra, _ := LoadJobDaily(filepath.Join(dataDir, "jobdaily_h5.txt"))
	return append(q, extra...)
}
