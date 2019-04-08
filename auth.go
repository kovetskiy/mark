package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/reconquest/karma-go"
	"github.com/zazab/zhash"
)

type Credentials struct {
	Username string
	Password string
	BaseURL  string
	PageID   string
}

func GetCredentials(
	args map[string]interface{},
	config zhash.Hash,
) (*Credentials, error) {
	var (
		username, _  = args["-u"].(string)
		password, _  = args["-p"].(string)
		targetURL, _ = args["-l"].(string)
	)

	var err error

	if username == "" {
		username, err = config.GetString("username")
		if err != nil {
			if zhash.IsNotFound(err) {
				return nil, errors.New(
					"Confluence username should be specified using -u " +
						"flag or be stored in configuration file",
				)
			}

			return nil, fmt.Errorf(
				"can't read username configuration variable: %s", err,
			)
		}
	}

	if password == "" {
		password, err = config.GetString("password")
		if err != nil {
			if zhash.IsNotFound(err) {
				return nil, errors.New(
					"Confluence password should be specified using -p " +
						"flag or be stored in configuration file",
				)
			}

			return nil, fmt.Errorf(
				"can't read password configuration variable: %s", err,
			)
		}
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
		var ok bool
		baseURL, ok = args["--base-url"].(string)
		if !ok {
			baseURL, err = config.GetString("base_url")
			if err != nil {
				if zhash.IsNotFound(err) {
					return nil, errors.New(
						"Confluence base URL should be specified using -l " +
							"flag or be stored in configuration file",
					)
				}

				return nil, fmt.Errorf(
					"can't read base_url configuration variable: %s", err,
				)
			}
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
