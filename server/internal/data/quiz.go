package data

import (
	"os"
	"path/filepath"
	"strings"
)

// Question 是猜谜的一题（npc_quiz.h NPC_QUIZ，setup.cf quizfile=data/question.txt）。
// AnswerType：1 二选一、2 三选一、4 写答案；Answer 是正确的选项（1..3，写答案时是 1）。
type Question struct {
	No, Type, Level, AnswerType, Answer int
	Text                                string
	Select                              [3]string
}

// LoadQuiz 是 QUIZ_initQuiz：每行 编号,种类,等级,答法,答案,题目,选项1,选项2,选项3；栏位不足 9 栏的行略过，
// 二选一却说答案是第 3 项、写答案却不是第 1 项的也略过（原版当作错误资料）。
func LoadQuiz(path string) ([]Question, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Question
	for _, line := range strings.Split(GBK(b), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || line[0] == '#' {
			continue
		}
		c := strings.Split(line, ",")
		if len(c) < 9 {
			continue
		}
		q := Question{No: Atoi(c[0]), Type: Atoi(c[1]), Level: Atoi(c[2]), AnswerType: Atoi(c[3]), Answer: Atoi(c[4]),
			Text: c[5], Select: [3]string{strings.TrimSpace(c[6]), strings.TrimSpace(c[7]), strings.TrimSpace(c[8])}}
		if q.AnswerType == 1 && q.Answer == 3 || q.AnswerType == 4 && q.Answer != 1 {
			continue
		}
		out = append(out, q)
	}
	return out, nil
}

func loadQuizFile(dataDir string) []Question {
	q, _ := LoadQuiz(filepath.Join(dataDir, "question.txt"))
	return q
}
