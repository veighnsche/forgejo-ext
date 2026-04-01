// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service_message

import (
	"context"
	"fmt"

	service_message_model "forgejo.org/models/service_message"
	service_message_module "forgejo.org/modules/service_message"
	"forgejo.org/modules/validation"
)

var SMTypeModal = "modal"

func NewServiceMessage(opts *service_message_module.ServiceMessageOptions) (*service_message_model.ServiceMessage, error) {
	sm := &service_message_model.ServiceMessage{
		Title: opts.Title,
		Text:  opts.Text,
		Type:  service_message_module.SMType("modal"),
	}
	if valid, err := validation.IsValid(sm); !valid {
		return &service_message_model.ServiceMessage{}, err
	}
	return sm, nil
}

func CreateOrUpdateServiceMessage(ctx context.Context, sm *service_message_model.ServiceMessage) error {
	return service_message_model.CreateOrUpdateServiceMessage(ctx, sm)
}

func GetServiceMessage(ctx context.Context, smType string) (*service_message_model.ServiceMessage, error) {
	smt := service_message_module.SMType(smType)
	if !smt.Valid() {
		return nil, fmt.Errorf("Invalid Service Message type %s", smType)
	}
	return service_message_model.GetServiceMessageByType(ctx, smt)
}

func DeleteServiceMessage(ctx context.Context, sm *service_message_model.ServiceMessage) error {
	return service_message_model.DeleteServiceMessage(ctx, sm)
}
