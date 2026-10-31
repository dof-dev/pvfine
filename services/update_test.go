package services

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
)

func TestUpdateServiceVersion(t *testing.T) {
	service := NewUpdateService(nil, "1.2.3")
	if got := service.Version(); got != "1.2.3" {
		t.Fatalf("Version() = %q, want %q", got, "1.2.3")
	}
}

type fakeUpdateClient struct {
	release    *updater.Release
	checkErr   error
	installErr error
	checks     int
	installs   int
}

func (c *fakeUpdateClient) Check(context.Context) (*updater.Release, error) {
	c.checks++
	return c.release, c.checkErr
}

func (c *fakeUpdateClient) CheckAndInstall(context.Context) error {
	c.installs++
	return c.installErr
}

func TestUpdateRequiresConfirmation(t *testing.T) {
	for _, background := range []bool{false, true} {
		t.Run(fmt.Sprintf("background=%v", background), func(t *testing.T) {
			client := &fakeUpdateClient{release: &updater.Release{Version: "1.2.4"}}
			var decide func(bool)
			service := &UpdateService{
				client: client,
				confirm: func(release *updater.Release, callback func(bool)) {
					if release.Version != "1.2.4" {
						t.Fatal("wrong release")
					}
					decide = callback
				},
				notify: func(string, string) {},
			}
			if err := service.checkForUpdates(background); err != nil {
				t.Fatal(err)
			}
			if decide == nil || client.installs != 0 {
				t.Fatal("download started before confirmation")
			}
			if err := service.CheckForUpdates(); err != nil {
				t.Fatal(err)
			}
			if client.checks != 1 {
				t.Fatal("concurrent check opened another prompt")
			}
			decide(false)
			decide(true)
			if client.installs != 0 || service.busy.Load() {
				t.Fatal("cancel did not stop update")
			}
			if err := service.CheckForUpdates(); err != nil {
				t.Fatal(err)
			}
			decide(true)
			decide(true)
			if client.installs != 1 || service.busy.Load() {
				t.Fatal("confirmation must start exactly one update")
			}
		})
	}
}

func TestUpdateCheckResults(t *testing.T) {
	for _, background := range []bool{false, true} {
		client := &fakeUpdateClient{}
		messages := 0
		service := &UpdateService{
			client:  client,
			confirm: func(*updater.Release, func(bool)) { t.Fatal("unexpected prompt") },
			notify:  func(string, string) { messages++ },
		}
		if err := service.checkForUpdates(background); err != nil {
			t.Fatal(err)
		}
		if (messages == 1) == background {
			t.Fatal("up-to-date feedback should only appear for manual checks")
		}
		client.checkErr = errors.New("check failed")
		if err := service.checkForUpdates(background); !errors.Is(err, client.checkErr) {
			t.Fatalf("check error = %v", err)
		}
		if service.busy.Load() || client.installs != 0 {
			t.Fatal("failed check left flow busy or started download")
		}
	}
}

func TestUpdateInstallFailureAllowsRetry(t *testing.T) {
	client := &fakeUpdateClient{release: &updater.Release{Version: "1.2.4"}, installErr: errors.New("download failed")}
	var reported string
	service := &UpdateService{
		client:  client,
		confirm: func(_ *updater.Release, decide func(bool)) { decide(true) },
		notify: func(kind, message string) {
			if kind == "error" {
				reported = message
			}
		},
	}
	if err := service.CheckForUpdates(); err != nil {
		t.Fatal(err)
	}
	if reported == "" || service.busy.Load() {
		t.Fatal("install failure was not reported or flow remained busy")
	}
	if err := service.CheckForUpdates(); err != nil {
		t.Fatal(err)
	}
	if client.installs != 2 {
		t.Fatal("retry did not start")
	}
}
