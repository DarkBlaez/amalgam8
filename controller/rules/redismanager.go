// Copyright 2016 IBM Corporation
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

package rules

import (
	"errors"

	"encoding/json"

	"github.com/Sirupsen/logrus"
	"github.com/amalgam8/amalgam8/pkg/api"
	"github.com/pborman/uuid"
)

// NewRedisManager creates a Redis backed manager implementation.
func NewRedisManager(host, pass string, v api.Validator) Manager {
	return &redisManager{
		validator: v,
		db:        newRedisDB(host, pass),
	}
}

type redisManager struct {
	validator api.Validator
	db        *redisDB
}

func (r *redisManager) AddRules(namespace string, rules []api.Rule) (NewRules, error) {
	if len(rules) == 0 {
		return NewRules{}, errors.New("rules: no rules provided")
	}

	// Validate rules
	for _, rule := range rules {
		if err := r.validator.Validate(rule); err != nil {
			return NewRules{}, &InvalidRuleError{}
		}
	}

	entries := make(map[string]string)
	for i := range rules {
		if rules[i].ID == "" {
			rules[i].ID = uuid.New() // Generate an ID for each rule
		}
		data, err := json.Marshal(&rules[i])
		if err != nil {
			return NewRules{}, &JSONMarshalError{Message: err.Error()}
		}

		entries[rules[i].ID] = string(data)
	}

	if err := r.db.InsertEntries(namespace, entries); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"namespace": namespace,
		}).Error("Error inserting entries in Redis")
		return NewRules{}, err
	}

	// Get the new IDs
	ids := make([]string, len(rules))
	for i, rule := range rules {
		ids[i] = rule.ID
	}

	return NewRules{
		IDs: ids,
	}, nil
}

func (r *redisManager) GetRules(namespace string, filter api.RuleFilter) (RetrievedRules, error) {
	results := []api.Rule{}

	var stringRules []string
	var err error
	var rev int64
	if len(filter.IDs) == 0 {
		stringRules, rev, err = r.db.ReadAllEntries(namespace)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"namespace": namespace,
				"filter":    filter,
			}).Error("Error reading all entries from redis")
			return RetrievedRules{}, err
		}
	} else {
		stringRules, rev, err = r.db.ReadEntries(namespace, filter.IDs)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"namespace": namespace,
				"filter":    filter,
			}).Error("Could not read entries from Redis")
			return RetrievedRules{}, err
		}
	}

	results = make([]api.Rule, len(stringRules))
	for index, entry := range stringRules {
		rule := api.Rule{}
		if err = json.Unmarshal([]byte(entry), &rule); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"namespace": namespace,
				"entry":     entry,
			}).Error("Could not unmarshal object returned from Redis")
			return RetrievedRules{}, &JSONMarshalError{Message: err.Error()}
		}
		results[index] = rule
	}

	results = filter.Apply(results)

	return RetrievedRules{
		Rules:    results,
		Revision: rev,
	}, nil
}

func (r *redisManager) UpdateRules(namespace string, rules []api.Rule) error {
	if len(rules) == 0 {
		return errors.New("rules: no rules provided")
	}

	// Validate rules
	for _, rule := range rules {
		if err := r.validator.Validate(rule); err != nil {
			return &InvalidRuleError{}
		}
	}

	entries := make(map[string]string)
	for _, rule := range rules {
		data, err := json.Marshal(&rule)
		if err != nil {
			return err
		}

		entries[rule.ID] = string(data)
	}

	if err := r.db.UpdateEntries(namespace, entries); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"namespace": namespace,
		}).Error("Error updating entries in Redis")
		return err
	}

	return nil
}

func (r *redisManager) DeleteRules(namespace string, filter api.RuleFilter) error {
	return r.db.DeleteEntriesByFilter(namespace, filter)
}
