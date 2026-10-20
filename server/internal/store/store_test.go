package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestStore(t *testing.T) {
	ctx := context.Background()
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	a, err := s.CreateAccount(ctx, "Alice", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAccount(ctx, "alice", "x"); !errors.Is(err, ErrExists) {
		t.Fatalf("账号名应不区分大小写唯一，得到 %v", err)
	}
	if _, err := s.Authenticate(ctx, "alice", "wrong"); !errors.Is(err, ErrBadLogin) {
		t.Fatal("错误密码应失败")
	}
	if _, err := s.Authenticate(ctx, "nobody", "x"); !errors.Is(err, ErrBadLogin) {
		t.Fatal("不存在的账号应失败")
	}
	if got, err := s.Authenticate(ctx, "ALICE", "secret"); err != nil || got.ID != a.ID {
		t.Fatalf("登录失败：%v", err)
	}

	c, err := s.CreateCharacter(ctx, Character{AccountID: a.ID, Name: "石器勇者", Sprite: 100000, Level: 1, Floor: 1006, X: 15, Y: 22})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCharacter(ctx, Character{AccountID: a.ID, Name: "石器勇者", Sprite: 100005}); !errors.Is(err, ErrExists) {
		t.Fatal("角色名应唯一")
	}
	if err := s.SavePosition(ctx, c.ID, 1000, 5, 6, 3); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetCharacter(ctx, a.ID, c.ID)
	if got.Floor != 1000 || got.X != 5 || got.Y != 6 || got.Dir != 3 {
		t.Fatalf("存档位置不对：%+v", got)
	}
	if _, err := s.GetCharacter(ctx, a.ID+1, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("不能读到别人账号下的角色")
	}
	if err := s.DeleteCharacter(ctx, a.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListCharacters(ctx, a.ID); len(list) != 0 {
		t.Fatal("删除后列表应为空")
	}
	if _, err := s.CreateCharacter(ctx, Character{AccountID: a.ID, Name: "石器勇者", Sprite: 100000}); err != nil {
		t.Fatalf("删除后名字应可重用：%v", err)
	}
}
