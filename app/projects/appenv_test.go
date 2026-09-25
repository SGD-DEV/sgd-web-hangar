package projects

import (
	"strings"
	"testing"
)

func TestAppEnvironment(t *testing.T) {
	p := Project{
		App:      &AppService{Command: "npm start", Port: 3100, Env: []string{"NODE_ENV=production", "PORT=4000"}},
		Database: &Database{Type: "postgresql", Host: "127.0.0.1", Port: 5432, Name: "app", User: "app", Password: "pw"},
	}
	env := strings.Join(AppEnvironment(p), "\n") + "\n"
	for _, want := range []string{
		"PORT=4000\n", // user line wins
		"NODE_ENV=production\n",
		"DB_CONNECTION=pgsql\n",
		"DATABASE_URL=postgresql://app:pw@127.0.0.1:5432/app\n",
		"MAIL_PORT=1025\n",
		"HOST=127.0.0.1\n",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("missing %q in\n%s", want, env)
		}
	}
	if strings.Count(env, "PORT=") != 3 { // PORT, DB_PORT, MAIL_PORT
		t.Errorf("duplicate keys:\n%s", env)
	}
}

func TestValidateApp(t *testing.T) {
	a := &AppService{Command: " npm start ", Port: 3100, Env: []string{"", "# comment", "A=1"}}
	if err := ValidateApp(a); err != nil || a.Command != "npm start" || len(a.Env) != 1 {
		t.Fatalf("got %v %+v", err, a)
	}
	for _, bad := range []*AppService{
		{Command: "", Port: 3100},
		{Command: "x", Port: 80},
		{Command: "a\nb", Port: 3100},
		{Command: "x", Port: 3100, Env: []string{"NOEQUALS"}},
	} {
		if ValidateApp(bad) == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}
