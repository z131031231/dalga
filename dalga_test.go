package dalga

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cenkalti/dalga/internal/log"
	"github.com/go-sql-driver/mysql"
)

func init() {
	log.EnableDebug()
}

const (
	testBody    = "testBody"
	testTimeout = 5 * time.Second
	testAddr    = "127.0.0.1:5000"
)

func dropTables(db *sql.DB, table string) error {
	dropSQL := "DROP TABLE " + table
	_, err := db.Exec(dropSQL)
	if err != nil {
		if myErr, ok := err.(*mysql.MySQLError); !ok || myErr.Number != 1051 { // Unknown table
			return err
		}
	}
	dropSQL = "DROP TABLE " + table + "_instances"
	_, err = db.Exec(dropSQL)
	if err != nil {
		if myErr, ok := err.(*mysql.MySQLError); !ok || myErr.Number != 1051 { // Unknown table
			return err
		}
	}
	println("dropped tables")
	return nil
}

func TestSchedule(t *testing.T) {
	config := DefaultConfig

	db, err := sql.Open("mysql", config.MySQL.DSN())
	if err != nil {
		t.Errorf(err.Error())
		return
	}

	defer db.Close()

	err = db.Ping()
	if err != nil {
		t.Fatalf("cannot connect to mysql: %s", err.Error())
	}
	println("connected to db")

	err = dropTables(db, config.MySQL.Table)
	if err != nil {
		t.Fatal(err)
	}

	called := make(chan string)
	endpoint := func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		buf.ReadFrom(r.Body)
		r.Body.Close()
		called <- buf.String()
	}

	http.HandleFunc("/", endpoint)
	go http.ListenAndServe(testAddr, nil)

	d, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	err = d.CreateTable()
	if err != nil {
		t.Fatal(err)
	}
	println("created table")
	defer dropTables(db, config.MySQL.Table)

	ctx, cancel := context.WithCancel(context.Background())
	go d.Run(ctx)
	defer func() {
		cancel()
		<-d.NotifyDone()
	}()

	values := make(url.Values)
	values.Set("one-off", "true")
	values.Set("first-run", "1990-01-01T00:00:00Z")

	scheduleURL := "http://" + config.Listen.Addr() + "/jobs/testPath/" + testBody
	req, err := http.NewRequest("PUT", scheduleURL, strings.NewReader(values.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var client http.Client
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("cannot schedule new job: %s", err.Error())
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	if resp.StatusCode != 201 {
		t.Fatalf("unexpected status code: %d, body: %q", resp.StatusCode, buf.String())
	}
	println("PUT response:", buf.String())

	println("scheduled job")

	select {
	case body := <-called:
		println("endpoint is called")
		if body != testBody {
			t.Fatalf("Invalid body: %s", body)
		}
	case <-time.After(testTimeout):
		t.Fatal("timeout")
	}
	time.Sleep(time.Second)
}
