package controllers

import (
	"context"
	"fmt"
	"github.com/hashicorp/vault/api"
	"github.com/mitchellh/go-homedir"
	"io/ioutil"
	"path/filepath"
	ctrl "sigs.k8s.io/controller-runtime"
	"time"
)

type VaultAuth struct {
	client  *api.Client
	path    string
	role    string
	jwtPath string
}

type kubernetesAuth struct {
	JWT  string `json:"jwt"`
	Role string `json:"role"`
}

var (
	vaultLog = ctrl.Log.WithName("vault")
)

func CreateVaultAuth(server string, path string, role string, jwtPath string) (*VaultAuth, error) {
	cfg := api.DefaultConfig()
	cfg.Address = server

	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	return &VaultAuth{
		client:  client,
		path:    path,
		role:    role,
		jwtPath: jwtPath,
	}, nil
}

func (auth *VaultAuth) authenticate() (*api.Secret, error) {
	jwt, err := ioutil.ReadFile(auth.jwtPath)
	if err != nil {
		return nil, err
	}

	request := auth.client.NewRequest("POST", fmt.Sprintf("/v1/auth/%s", auth.path))
	err = request.SetJSONBody(&kubernetesAuth{
		JWT:  string(jwt),
		Role: auth.role,
	})
	if err != nil {
		return nil, err
	}

	response, err := auth.client.RawRequest(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.Error() != nil {
		return nil, response.Error()
	}

	secret, err := api.ParseSecret(response.Body)
	if err != nil {
		return nil, err
	}

	return secret, nil
}

func (auth *VaultAuth) writeToken(secret *api.Secret) error {
	homePath, err := homedir.Dir()
	if err != nil {
		return err
	}
	tokenPath := filepath.Join(homePath, ".vault-token")
	if err = ioutil.WriteFile(tokenPath, []byte(secret.Auth.ClientToken), 0600); err != nil {
		return err
	}
	return nil
}

func (auth *VaultAuth) StartAutoRenew(ctx context.Context) {
	for {
		err := auth.autoRenewal(ctx)

		// if any error happened, wait for 30s before next attempt
		if err == nil {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		} else {
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
				continue
			}
		}
	}
}

func (auth *VaultAuth) autoRenewal(ctx context.Context) error {
	initial, err := auth.authenticate()
	if err != nil {
		vaultLog.Error(err, "could not authenticate with vault")
		return err
	}

	err = auth.writeToken(initial)
	if err != nil {
		vaultLog.Error(err, "could not write auth token")
		return err
	}

	vaultLog.Info("vault token updated")

	watcher, err := auth.client.NewLifetimeWatcher(&api.LifetimeWatcherInput{Secret: initial})
	if err != nil {
		return err
	}

	go watcher.Start()
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err = <-watcher.DoneCh():
			if err != nil {
				vaultLog.Error(err, "could not renew vault token")
			}
			return err
		case <-watcher.RenewCh():
			vaultLog.Info("vault token renewed")
		}
	}
}
