package mgmt

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var activeHedgeCloseStatuses = []string{HedgeCloseRequested, HedgeCloseSubmitted, HedgeCloseVerifying}

func (r *Repository) ListActiveHedgeCloseRequests(ctx context.Context) ([]HedgeCloseRequest, error) {
	var requests []HedgeCloseRequest
	if err := r.db.WithContext(ctx).
		Where("status IN ?", activeHedgeCloseStatuses).
		Order("id ASC").
		Find(&requests).Error; err != nil {
		return nil, fmt.Errorf("list active hedge close requests: %w", err)
	}
	return requests, nil
}

func (r *Repository) BeginHedgeCloseRequest(ctx context.Context, configID uint, idempotencyKey string) (HedgeCloseRequest, bool, error) {
	var request HedgeCloseRequest
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var config HedgeConfig
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&config, configID).Error; err != nil {
			return err
		}
		err := tx.Where("config_id = ? AND status IN ?", configID, activeHedgeCloseStatuses).
			Order("id DESC").First(&request).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		request = HedgeCloseRequest{
			ConfigID: configID, IdempotencyKey: idempotencyKey,
			Status: HedgeCloseRequested, RequestedAt: time.Now().UTC(),
		}
		if err := tx.Create(&request).Error; err != nil {
			return err
		}
		if err := tx.Model(&config).Updates(map[string]any{
			"enabled": true, "lifecycle_status": HedgeLifecycleClosing,
		}).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return HedgeCloseRequest{}, false, ErrNotFound
	}
	if err != nil {
		return HedgeCloseRequest{}, false, fmt.Errorf("begin hedge close request: %w", err)
	}
	return request, created, nil
}

func (r *Repository) AttachHedgeCloseExecution(ctx context.Context, requestID uint, executionID int64, status string) error {
	result := r.db.WithContext(ctx).Model(&HedgeCloseRequest{}).
		Where("id = ? AND status IN ?", requestID, activeHedgeCloseStatuses).
		Updates(map[string]any{"order_execution_id": executionID, "status": status, "error_message": ""})
	if result.Error != nil {
		return fmt.Errorf("attach hedge close execution: %w", result.Error)
	}
	return nil
}

func (r *Repository) GetHedgeCloseRequestByExecutionID(ctx context.Context, executionID int64) (HedgeCloseRequest, error) {
	var request HedgeCloseRequest
	err := r.db.WithContext(ctx).Where("order_execution_id = ?", executionID).Order("id DESC").First(&request).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return HedgeCloseRequest{}, ErrNotFound
	}
	if err != nil {
		return HedgeCloseRequest{}, fmt.Errorf("get hedge close request: %w", err)
	}
	return request, nil
}

func (r *Repository) UpdateHedgeCloseStatus(ctx context.Context, requestID uint, status string) error {
	if err := r.db.WithContext(ctx).Model(&HedgeCloseRequest{}).Where("id = ?", requestID).
		Updates(map[string]any{"status": status, "error_message": ""}).Error; err != nil {
		return fmt.Errorf("update hedge close status: %w", err)
	}
	return nil
}

func (r *Repository) CompleteHedgeClose(ctx context.Context, requestID uint) error {
	return r.finishHedgeClose(ctx, requestID, HedgeCloseCompleted, HedgeLifecycleDisabled, "")
}

func (r *Repository) FailHedgeClose(ctx context.Context, requestID uint, message string) error {
	return r.finishHedgeClose(ctx, requestID, HedgeCloseFailed, HedgeLifecycleCloseFailed, message)
}

func (r *Repository) finishHedgeClose(ctx context.Context, requestID uint, requestStatus, lifecycleStatus, message string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var request HedgeCloseRequest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&request, requestID).Error; err != nil {
			return err
		}
		updates := map[string]any{"status": requestStatus, "error_message": message}
		if requestStatus == HedgeCloseCompleted {
			now := time.Now().UTC()
			updates["completed_at"] = now
		}
		if err := tx.Model(&request).Updates(updates).Error; err != nil {
			return err
		}
		configUpdates := map[string]any{"lifecycle_status": lifecycleStatus}
		if lifecycleStatus == HedgeLifecycleDisabled {
			configUpdates["enabled"] = false
		}
		if err := tx.Model(&HedgeConfig{}).Where("id = ?", request.ConfigID).Updates(configUpdates).Error; err != nil {
			return err
		}
		return nil
	})
}
