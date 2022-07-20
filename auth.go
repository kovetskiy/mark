package main

import (
	"errors"
	"io/ioutil"
	"net/url"
	"os"
	"strings"

	"github.com/reconquest/karma-go"
)

type Credentials struct {
	Username string
	Password string
	BaseURL  string
	PageID   string
}

func GetCredentials(
	flags Flags,
	config *Config,
) (*Credentials, error) {
	var err error

	var (
		username  = flags.Username
		password  = flags.Password
		targetURL = flags.TargetURL
	)

	if username == "" {
		username = config.Username
	}

	if password == "" {
		password = config.Password
		if password == "" {
			if ! flags.CompileOnly {
				return nil, errors.New(
					"Confluence password should be specified using -p " +
						"flag or be stored in configuration file",
				)
			}
			password = "none"
		}
	}

	if password == "-" {
		stdin, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			return nil, karma.Format(
				err,
				"unable to read password from stdin",
			)
		}

		password = string(stdin)
	}

	if flags.CompileOnly && targetURL == "" {
		targetURL = "http://localhost"
	}

	url, err := url.Parse(targetURL)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to parse %q as url", targetURL,
		)
	}

	baseURL := url.Scheme + "://" + url.Host

	if url.Host == "" {
		baseURL = flags.BaseURL
		if baseURL == "" {
			baseURL = config.BaseURL
		}

		if baseURL == "" {
			return nil, errors.New(
				"Confluence base URL should be specified using -l " +
					"flag or be stored in configuration file",
			)
		}
	}

	baseURL = strings.TrimRight(baseURL, `/`)

	pageID := url.Query().Get("pageId")

	creds := &Credentials{
		Username: username,
		Password: password,
		BaseURL:  baseURL,
		PageID:   pageID,
	}

	return creds, nil
}
