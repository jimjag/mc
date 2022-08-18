// Copyright (c) 2015-2021 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package ilm

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/minio/cli"
	"github.com/minio/mc/pkg/probe"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
	"github.com/rs/xid"
)

const defaultILMDateFormat string = "2006-01-02"

// Align text in label to center, pad with spaces on either sides.
func getCenterAligned(label string, maxLen int) string {
	const toPadWith string = " "
	lblLth := len(label)
	if lblLth > 1 && lblLth%2 != 0 {
		lblLth++
	} else if lblLth == 1 {
		lblLth = 2
	}
	length := (float64(maxLen - lblLth)) / float64(2)
	rptLth := (int)(math.Floor(length / float64(len(toPadWith))))
	leftRptLth := rptLth
	rightRptLth := rptLth
	if rptLth <= 0 {
		leftRptLth = 1
		rightRptLth = 0
	}
	output := strings.Repeat(toPadWith, leftRptLth) + label + strings.Repeat(toPadWith, rightRptLth)
	return output
}

// Align text in label to left, pad with spaces.
func getLeftAligned(label string, maxLen int) string {
	const toPadWith string = " "
	lblLth := len(label)
	length := maxLen - lblLth
	if length <= 0 {
		return label
	}
	output := strings.Repeat(toPadWith, 1) + label + strings.Repeat(toPadWith, length-1)
	return output
}

// Align text in label to right, pad with spaces.
func getRightAligned(label string, maxLen int) string {
	const toPadWith string = " "
	lblLth := len(label)
	length := maxLen - lblLth
	if length <= 0 {
		return label
	}
	output := strings.Repeat(toPadWith, length) + label
	return output
}

// RemoveILMRule - Remove the ILM rule (with ilmID) from the configuration in XML that is provided.
func RemoveILMRule(lfcCfg *lifecycle.Configuration, ilmID string) (*lifecycle.Configuration, *probe.Error) {
	if lfcCfg == nil {
		return lfcCfg, probe.NewError(fmt.Errorf("lifecycle configuration not set"))
	}
	if len(lfcCfg.Rules) == 0 {
		return lfcCfg, probe.NewError(fmt.Errorf("lifecycle configuration not set"))
	}
	n := 0
	for _, rule := range lfcCfg.Rules {
		if rule.ID != ilmID {
			lfcCfg.Rules[n] = rule
			n++
		}
	}
	if n == len(lfcCfg.Rules) && len(lfcCfg.Rules) > 0 {
		// if there was no filtering then rules will be of same length, means we didn't find
		// our ilm id return an error here.
		return lfcCfg, probe.NewError(fmt.Errorf("lifecycle rule for id '%s' not found", ilmID))
	}
	lfcCfg.Rules = lfcCfg.Rules[:n]
	return lfcCfg, nil
}

// LifecycleOptions is structure to encapsulate
type LifecycleOptions struct {
	ID string

	Status *bool

	Prefix         *string
	Tags           *string
	ExpiryDate     *string
	ExpiryDays     *string
	TransitionDate *string
	TransitionDays *string
	StorageClass   *string

	ExpiredObjectDeleteMarker               *bool
	NoncurrentVersionExpirationDays         *int
	NewerNoncurrentExpirationVersions       *int
	NoncurrentVersionTransitionDays         *int
	NewerNoncurrentTransitionVersions       *int
	NoncurrentVersionTransitionStorageClass *string
}

// ToILMRule creates lifecycle.Configuration based on LifecycleOptions
func (opts LifecycleOptions) ToILMRule(config *lifecycle.Configuration) (lifecycle.Rule, *probe.Error) {
	var (
		id, status string

		filter lifecycle.Filter

		nonCurrentVersionExpirationDays         lifecycle.ExpirationDays
		newerNonCurrentExpirationVersions       int
		nonCurrentVersionTransitionDays         lifecycle.ExpirationDays
		newerNonCurrentTransitionVersions       int
		nonCurrentVersionTransitionStorageClass string
	)

	id = opts.ID
	status = func() string {
		if opts.Status != nil && *opts.Status == false {
			return "Disabled"
		}
		// Generating a new ILM rule without explicit status is enabled
		return "Enabled"
	}()

	expiry, err := parseExpiry(opts.ExpiryDate, opts.ExpiryDays, opts.ExpiredObjectDeleteMarker)
	if err != nil {
		return lifecycle.Rule{}, err
	}

	transition, err := parseTransition(opts.StorageClass, opts.TransitionDate, opts.TransitionDays)
	if err != nil {
		return lifecycle.Rule{}, err
	}

	andVal := lifecycle.And{}
	if opts.Tags != nil {
		andVal.Tags = extractILMTags(*opts.Tags)
	}

	if opts.Prefix != nil {
		filter.Prefix = *opts.Prefix
	}

	if len(andVal.Tags) > 0 {
		filter.And = andVal
		if opts.Prefix != nil {
			filter.And.Prefix = *opts.Prefix
		}
		filter.Prefix = ""
	}

	if opts.NoncurrentVersionExpirationDays != nil {
		nonCurrentVersionExpirationDays = lifecycle.ExpirationDays(*opts.NoncurrentVersionExpirationDays)
	}
	if opts.NewerNoncurrentExpirationVersions != nil {
		newerNonCurrentExpirationVersions = *opts.NewerNoncurrentExpirationVersions
	}
	if opts.NoncurrentVersionTransitionDays != nil {
		nonCurrentVersionTransitionDays = lifecycle.ExpirationDays(*opts.NoncurrentVersionTransitionDays)
	}
	if opts.NewerNoncurrentTransitionVersions != nil {
		newerNonCurrentTransitionVersions = *opts.NewerNoncurrentTransitionVersions
	}
	if opts.NoncurrentVersionTransitionStorageClass != nil {
		nonCurrentVersionTransitionStorageClass = *opts.NoncurrentVersionTransitionStorageClass
	}

	newRule := lifecycle.Rule{
		ID:         id,
		RuleFilter: filter,
		Status:     status,
		Expiration: expiry,
		Transition: transition,
		NoncurrentVersionExpiration: lifecycle.NoncurrentVersionExpiration{
			NoncurrentDays:          nonCurrentVersionExpirationDays,
			NewerNoncurrentVersions: newerNonCurrentExpirationVersions,
		},
		NoncurrentVersionTransition: lifecycle.NoncurrentVersionTransition{
			NoncurrentDays:          nonCurrentVersionTransitionDays,
			NewerNoncurrentVersions: newerNonCurrentTransitionVersions,
			StorageClass:            nonCurrentVersionTransitionStorageClass,
		},
	}

	if err := validateILMRule(newRule); err != nil {
		return lifecycle.Rule{}, err
	}

	return newRule, nil
}

func strPtr(s string) *string {
	ptr := s
	return &ptr
}

func intPtr(i int) *int {
	ptr := i
	return &ptr
}

func boolPtr(b bool) *bool {
	ptr := b
	return &ptr
}

// GetLifecycleOptions create LifeCycleOptions based on cli inputs
func GetLifecycleOptions(ctx *cli.Context) (LifecycleOptions, *probe.Error) {
	var (
		id string

		status *bool

		prefix         *string
		tags           *string
		expiryDate     *string
		expiryDays     *string
		transitionDate *string
		transitionDays *string
		sc             *string

		expiredObjectDeleteMarker         *bool
		noncurrentVersionExpirationDays   *int
		newerNoncurrentExpirationVersions *int
		noncurrentVersionTransitionDays   *int
		newerNoncurrentTransitionVersions *int
		noncurrentSC                      *string
	)

	id = ctx.String("id")
	if id == "" {
		id = xid.New().String()
	}

	switch {
	case ctx.IsSet("disable"):
		status = boolPtr(!ctx.Bool("disable"))
	case ctx.IsSet("enable"):
		status = boolPtr(ctx.Bool("enable"))
	}

	if ctx.IsSet("prefix") {
		prefix = strPtr(ctx.String("prefix"))
	} else {
		// Calculating the prefix for the aliased URL is deprecated in Aug 2022
		// split the first arg i.e. path into alias, bucket and prefix
		result := strings.SplitN(ctx.Args().First(), "/", 3)
		// get the prefix from path
		if len(result) > 2 {
			p := result[len(result)-1]
			if len(p) > 0 {
				prefix = &p
			}
		}
	}

	if ctx.IsSet("storage-class") {
		sc = strPtr(strings.ToUpper(ctx.String("storage-class")))
	}
	if ctx.IsSet("noncurrentversion-transition-storage-class") {
		noncurrentSC = strPtr(strings.ToUpper(ctx.String("noncurrentversion-transition-storage-class")))
	}
	if sc != nil && !ctx.IsSet("transition-days") && !ctx.IsSet("transition-date") {
		return LifecycleOptions{}, probe.NewError(errors.New("transition-date or transition-days must be set"))
	}
	if noncurrentSC != nil && !ctx.IsSet("noncurrentversion-transition-days") {
		return LifecycleOptions{}, probe.NewError(errors.New("noncurrentversion-transition-days must be set"))
	}
	// for MinIO transition storage-class is same as label defined on
	// `mc admin bucket remote add --service ilm --label` command
	if ctx.IsSet("tags") {
		tags = strPtr(ctx.String("tags"))
	}
	if ctx.IsSet("expiry-date") {
		expiryDate = strPtr(ctx.String("expiry-date"))
	}
	if ctx.IsSet("expiry-days") {
		expiryDays = strPtr(ctx.String("expiry-days"))
	}
	if ctx.IsSet("transition-date") {
		transitionDate = strPtr(ctx.String("transition-date"))
	}
	if ctx.IsSet("transition-days") {
		transitionDays = strPtr(ctx.String("transition-days"))
	}
	if ctx.IsSet("expired-object-delete-marker") {
		expiredObjectDeleteMarker = boolPtr(ctx.Bool("expired-object-delete-marker"))
	}
	if ctx.IsSet("noncurrentversion-expiration-days") {
		noncurrentVersionExpirationDays = intPtr(ctx.Int("noncurrentversion-expiration-days"))
	}
	if ctx.IsSet("newer-noncurrentversions-expiration") {
		newerNoncurrentExpirationVersions = intPtr(ctx.Int("newer-noncurrentversions-expiration"))
	}
	if ctx.IsSet("noncurrentversion-transition-days") {
		noncurrentVersionTransitionDays = intPtr(ctx.Int("noncurrentversion-transition-days"))
	}
	if ctx.IsSet("newer-noncurrentversions-transition") {
		newerNoncurrentTransitionVersions = intPtr(ctx.Int("newer-noncurrentversions-transition"))
	}

	return LifecycleOptions{
		ID:                                      id,
		Status:                                  status,
		Prefix:                                  prefix,
		Tags:                                    tags,
		ExpiryDate:                              expiryDate,
		ExpiryDays:                              expiryDays,
		TransitionDate:                          transitionDate,
		TransitionDays:                          transitionDays,
		StorageClass:                            sc,
		ExpiredObjectDeleteMarker:               expiredObjectDeleteMarker,
		NoncurrentVersionExpirationDays:         noncurrentVersionExpirationDays,
		NewerNoncurrentExpirationVersions:       newerNoncurrentExpirationVersions,
		NoncurrentVersionTransitionDays:         noncurrentVersionTransitionDays,
		NewerNoncurrentTransitionVersions:       newerNoncurrentTransitionVersions,
		NoncurrentVersionTransitionStorageClass: noncurrentSC,
	}, nil
}

// ApplyRuleFields applies non nil fields of LifcycleOptions to the existing lifecycle rule
func ApplyRuleFields(dest *lifecycle.Rule, opts LifecycleOptions) *probe.Error {
	// If src has tags, it should override the destination
	if opts.Tags != nil {
		dest.RuleFilter.And.Tags = extractILMTags(*opts.Tags)
	}

	// since prefix is a part of command args, it is always present in the src rule and
	// it should be always set to the destination.
	if opts.Prefix != nil {
		if dest.RuleFilter.And.Tags != nil {
			dest.RuleFilter.And.Prefix = *opts.Prefix
		} else {
			dest.RuleFilter.Prefix = *opts.Prefix
		}
	}

	// only one of expiration day, date or transition day, date is expected
	if opts.ExpiryDate != nil {
		date, err := parseExpiryDate(*opts.ExpiryDate)
		if err != nil {
			return err
		}
		dest.Expiration.Date = date
		// reset everything else
		dest.Expiration.Days = 0
		dest.Expiration.DeleteMarker = false
	} else if opts.ExpiryDays != nil {
		days, err := parseExpiryDays(*opts.ExpiryDays)
		if err != nil {
			return err
		}
		dest.Expiration.Days = days
		// reset everything else
		dest.Expiration.Date = lifecycle.ExpirationDate{}
	} else if opts.ExpiredObjectDeleteMarker != nil {
		dest.Expiration.DeleteMarker = lifecycle.ExpireDeleteMarker(*opts.ExpiredObjectDeleteMarker)
		dest.Expiration.Days = 0
		dest.Expiration.Date = lifecycle.ExpirationDate{}
	}

	if opts.TransitionDate != nil {
		date, err := parseTransitionDate(*opts.TransitionDate)
		if err != nil {
			return err
		}
		dest.Transition.Date = date
		// reset everything else
		dest.Transition.Days = 0
	} else if opts.TransitionDays != nil {
		days, err := parseTransitionDays(*opts.TransitionDays)
		if err != nil {
			return err
		}
		dest.Transition.Days = days
		// reset everything else
		dest.Transition.Date = lifecycle.ExpirationDate{}
	}

	if opts.NoncurrentVersionExpirationDays != nil {
		dest.NoncurrentVersionExpiration.NoncurrentDays = lifecycle.ExpirationDays(*opts.NoncurrentVersionExpirationDays)
	}

	if opts.NewerNoncurrentExpirationVersions != nil {
		dest.NoncurrentVersionExpiration.NewerNoncurrentVersions = *opts.NewerNoncurrentExpirationVersions
	}

	if opts.NoncurrentVersionTransitionDays != nil {
		dest.NoncurrentVersionTransition.NoncurrentDays = lifecycle.ExpirationDays(*opts.NoncurrentVersionTransitionDays)
	}

	if opts.NewerNoncurrentTransitionVersions != nil {
		dest.NoncurrentVersionTransition.NewerNoncurrentVersions = *opts.NewerNoncurrentTransitionVersions
	}

	if opts.NoncurrentVersionTransitionStorageClass != nil {
		dest.NoncurrentVersionTransition.StorageClass = *opts.NoncurrentVersionTransitionStorageClass
	}

	if opts.StorageClass != nil {
		dest.Transition.StorageClass = *opts.StorageClass
	}

	// Updated the status
	if opts.Status != nil {
		dest.Status = func() string {
			if *opts.Status {
				return "Enabled"
			}
			return "Disabled"
		}()
	}

	return nil
}
