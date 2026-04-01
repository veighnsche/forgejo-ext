// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service_message

import (
	"context"
	"fmt"

	"forgejo.org/models/db"
	"forgejo.org/modules/log"
	service_message_types "forgejo.org/modules/service_message"
	"forgejo.org/modules/timeutil"
	"forgejo.org/modules/validation"
)

func init() {
	db.RegisterModel(new(ServiceMessage))
}

type ServiceMessage struct {
	ID          int64                        `xorm:"pk autoincr"`
	Title       string                       `xorm:"NOT NULL"`
	Text        string                       `xorm:"LONGTEXT"`
	Type        service_message_types.SMType `xorm:"INDEX UNIQUE NOT NULL"`
	CreatedUnix timeutil.TimeStamp           `xorm:"created"`
	UpdatedUnix timeutil.TimeStamp           `xorm:"updated"`
}

func (ServiceMessage) TableName() string {
	return "service_message"
}

func (sm ServiceMessage) Validate() []string {
	var result []string
	result = append(result, validation.ValidateNotEmpty(sm.Text, "Text")...)
	if !sm.Type.Valid() {
		result = append(result, service_message_types.ErrInvalidServiceMessageType.Error())
	}
	return result
}

func ServiceMessageExists(ctx context.Context, smType service_message_types.SMType, id ...int64) (bool, error) {
	if len(id) > 0 {
		return db.GetEngine(ctx).Where("type = ? OR id = ?", smType.Name(), id[0]).Get(&ServiceMessage{})
	}
	return db.GetEngine(ctx).Where("type = ?", smType.Name()).Get(&ServiceMessage{})
}

// CreateOrUpdateServiceMessage creates record of a ServiceMessage, expects a valid ServiceMessage
func CreateOrUpdateServiceMessage(ctx context.Context, sm *ServiceMessage) error {
	// Update if exists
	exists, err := ServiceMessageExists(ctx, sm.Type, sm.ID)
	if err != nil {
		return err
	} else if exists {
		e := db.GetEngine(ctx)
		oldSM, err := GetServiceMessageByType(ctx, sm.Type)
		if err != nil {
			return err
		}
		_, err = e.ID(oldSM.ID).AllCols().Update(sm)
		log.Debug("Existing Service Message %s was updated.", sm.Type.Name())
		return err
	}

	ctx, committer, err := db.TxContext(ctx)
	if err != nil {
		return err
	}
	defer committer.Close()

	if err = db.Insert(ctx, sm); err != nil {
		return fmt.Errorf("insert service message: %w", err)
	}
	log.Debug("Created service message of type %q", sm.Type.Name())
	return committer.Commit()
}

func GetServiceMessageByType(ctx context.Context, smType service_message_types.SMType) (*ServiceMessage, error) {
	sm := &ServiceMessage{}
	has, err := db.GetEngine(ctx).Where("type = ?", smType.Name()).Get(sm)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, service_message_types.ErrServiceMessageNotExist
	}
	log.Debug("Got service message %q", smType)
	return sm, nil
}

// Delete a remote registry in the DB, expects a valid rr
func DeleteServiceMessage(ctx context.Context, sm *ServiceMessage) error {
	_, err := db.GetEngine(ctx).ID(sm.ID).Delete(sm)
	if err != nil {
		return err
	}

	log.Debug("Deleted service message %q", sm.Type.Name())
	return nil
}
