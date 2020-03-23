/*
 * MinIO Client, (C) 2015-2019 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/minio/cli"
	json "github.com/minio/mc/pkg/colorjson"
	"github.com/minio/mc/pkg/probe"
	"github.com/minio/minio/pkg/console"
)

// mirror specific flags.
var (
	mirrorFlags = []cli.Flag{
		cli.BoolFlag{
			Name:   "force",
			Usage:  "force allows forced overwrite or removal of object(s) on target",
			Hidden: true, // Hidden since this option is deprecated.
		},
		cli.BoolFlag{
			Name:  "overwrite",
			Usage: "overwrite object(s) on target",
		},
		cli.BoolFlag{
			Name:  "fake",
			Usage: "perform a fake mirror operation",
		},
		cli.BoolFlag{
			Name:  "watch, w",
			Usage: "watch and synchronize changes",
		},
		cli.BoolFlag{
			Name:  "remove",
			Usage: "remove extraneous object(s) on target",
		},
		cli.StringFlag{
			Name:  "region",
			Usage: "specify region when creating new bucket(s) on target",
			Value: "us-east-1",
		},
		cli.BoolFlag{
			Name:  "preserve, a",
			Usage: "preserve file(s)/object(s) attributes and bucket policy rules on target bucket(s)",
		},
		cli.StringFlag{
			Name:  "multi-master",
			Usage: `multi-master multi-site setup, "value" is the site tag for the multi-master deployment`,
		},
		cli.StringSliceFlag{
			Name:  "exclude",
			Usage: "exclude object(s) that match specified object name pattern",
		},
		cli.StringFlag{
			Name:  "older-than",
			Usage: "filter object(s) older than L days, M hours and N minutes",
		},
		cli.StringFlag{
			Name:  "newer-than",
			Usage: "filter object(s) newer than L days, M hours and N minutes",
		},
		cli.StringFlag{
			Name:  "storage-class, sc",
			Usage: "specify storage class for new object(s) on target",
		},
		cli.StringFlag{
			Name:  "encrypt",
			Usage: "encrypt/decrypt objects (using server-side encryption with server managed keys)",
		},
		cli.StringFlag{
			Name:  "attr",
			Usage: "add custom metadata for all objects",
		},
	}
)

//  Mirror folders recursively from a single source to many destinations
var mirrorCmd = cli.Command{
	Name:   "mirror",
	Usage:  "synchronize object(s) to a remote site",
	Action: mainMirror,
	Before: setGlobalsFromContext,
	Flags:  append(append(mirrorFlags, ioFlags...), globalFlags...),
	CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} [FLAGS] SOURCE TARGET

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
ENVIRONMENT VARIABLES:
   MC_ENCRYPT:      list of comma delimited prefixes
   MC_ENCRYPT_KEY:  list of comma delimited prefix=secret values

EXAMPLES:
  01. Mirror a bucket recursively from MinIO cloud storage to a bucket on Amazon S3 cloud storage.
      {{.Prompt}} {{.HelpName}} play/photos/2014 s3/backup-photos

  02. Mirror a local folder recursively to Amazon S3 cloud storage.
      {{.Prompt}} {{.HelpName}} backup/ s3/archive

  03. Only mirror files that are newer than 7 days, 10 hours and 30 minutes to Amazon S3 cloud storage.
      {{.Prompt}} {{.HelpName}} --newer-than "7d10h30m" backup/ s3/archive

  04. Mirror a bucket from aliased Amazon S3 cloud storage to a folder on Windows.
      {{.Prompt}} {{.HelpName}} s3\documents\2014\ C:\backup\2014

  05. Mirror a bucket from aliased Amazon S3 cloud storage to a local folder use '--overwrite' to overwrite destination.
      {{.Prompt}} {{.HelpName}} --overwrite s3/miniocloud miniocloud-backup

  06. Mirror a bucket from MinIO cloud storage to a bucket on Amazon S3 cloud storage and remove any extraneous
      files on Amazon S3 cloud storage.
      {{.Prompt}} {{.HelpName}} --remove play/photos/2014 s3/backup-photos/2014

  07. Continuously mirror a local folder recursively to MinIO cloud storage. '--watch' continuously watches for
      new objects, uploads and removes extraneous files on Amazon S3 cloud storage.
      {{.Prompt}} {{.HelpName}} --remove --watch /var/lib/backups play/backups

  08. Mirror a bucket from aliased Amazon S3 cloud storage to a local folder.
      Exclude all .* files and *.temp files when mirroring.
      {{.Prompt}} {{.HelpName}} --exclude ".*" --exclude "*.temp" s3/test ~/test

  09. Mirror objects newer than 10 days from bucket test to a local folder.
      {{.Prompt}} {{.HelpName}} --newer-than 10d s3/test ~/localfolder

  10. Mirror objects older than 30 days from Amazon S3 bucket test to a local folder.
      {{.Prompt}} {{.HelpName}} --older-than 30d s3/test ~/test

  11. Mirror server encrypted objects from MinIO cloud storage to a bucket on Amazon S3 cloud storage
      {{.Prompt}} {{.HelpName}} --encrypt-key "minio/photos=32byteslongsecretkeymustbegiven1,s3/archive=32byteslongsecretkeymustbegiven2" minio/photos/ s3/archive/

  12. Mirror server encrypted objects from MinIO cloud storage to a bucket on Amazon S3 cloud storage. In case the encryption key contains
      non-printable character like tab, pass the base64 encoded string as key.
      {{.Prompt}} {{.HelpName}} --encrypt-key "s3/photos/=32byteslongsecretkeymustbegiven1,play/archive/=MzJieXRlc2xvbmdzZWNyZXRrZQltdXN0YmVnaXZlbjE=" s3/photos/ play/archive/

  13. Update 'Cache-Control' header on all existing objects recursively.
      {{.Prompt}} {{.HelpName}} --attr "Cache-Control=max-age=90000,min-fresh=9000" myminio/video-files myminio/video-files

  14. Mirror a local folder recursively to Amazon S3 cloud storage and preserve all local file attributes.
      {{.Prompt}} {{.HelpName}} -a backup/ s3/archive

  15. Cross mirror between sites in a multi-master deployment.
      Site-A: {{.Prompt}} {{.HelpName}} --watch --multi-master splunk-smartstore1 siteA siteB
      Site-B: {{.Prompt}} {{.HelpName}} --watch --multi-master splunk-smartstore1 siteB siteA
`,
}

const uaMirrorAppName = "mc-mirror"

type mirrorJob struct {
	stopCh chan struct{}

	// mutex for shutdown, this prevents the shutdown
	// to be initiated multiple times
	m sync.Mutex

	// the global watcher object, which receives notifications of created
	// and deleted files
	watcher *Watcher

	// Hold operation status information
	status Status

	queueCh  chan func() URLs
	parallel *ParallelManager

	// channel for status messages
	statusCh chan URLs

	TotalObjects int64
	TotalBytes   int64

	sourceURL string
	targetURL string

	isFake, isRemove, isOverwrite bool
	isWatch, isPreserve           bool
	olderThan, newerThan          string
	storageClass                  string
	userMetadata                  map[string]string

	excludeOptions []string
	encKeyDB       map[string][]prefixSSEPair

	multiMasterEnable bool
	multiMasterSTag   string
}

// mirrorMessage container for file mirror messages
type mirrorMessage struct {
	Status     string `json:"status"`
	Source     string `json:"source"`
	Target     string `json:"target"`
	Size       int64  `json:"size"`
	TotalCount int64  `json:"totalCount"`
	TotalSize  int64  `json:"totalSize"`
}

// String colorized mirror message
func (m mirrorMessage) String() string {
	return console.Colorize("Mirror", fmt.Sprintf("`%s` -> `%s`", m.Source, m.Target))
}

// JSON jsonified mirror message
func (m mirrorMessage) JSON() string {
	m.Status = "success"
	mirrorMessageBytes, e := json.MarshalIndent(m, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")

	return string(mirrorMessageBytes)
}

// doRemove - removes files on target.
func (mj *mirrorJob) doRemove(sURLs URLs) URLs {
	if mj.isFake {
		return sURLs.WithError(nil)
	}

	// Construct proper path with alias.
	targetWithAlias := filepath.Join(sURLs.TargetAlias, sURLs.TargetContent.URL.Path)
	clnt, pErr := newClient(targetWithAlias)
	if pErr != nil {
		return sURLs.WithError(pErr)
	}
	clnt.AddUserAgent(uaMirrorAppName, Version)
	contentCh := make(chan *clientContent, 1)
	contentCh <- &clientContent{URL: *newClientURL(sURLs.TargetContent.URL.Path)}
	close(contentCh)
	isRemoveBucket := false
	errorCh := clnt.Remove(false, isRemoveBucket, contentCh)
	for pErr := range errorCh {
		if pErr != nil {
			switch pErr.ToGoError().(type) {
			case PathInsufficientPermission:
				// Ignore Permission error.
				continue
			}
			return sURLs.WithError(pErr)
		}
	}

	return sURLs.WithError(nil)
}

// doMirror - Mirror an object to multiple destination. URLs status contains a copy of sURLs and error if any.
func (mj *mirrorJob) doMirror(ctx context.Context, cancelMirror context.CancelFunc, sURLs URLs) URLs {

	if sURLs.Error != nil { // Erroneous sURLs passed.
		return sURLs.WithError(sURLs.Error.Trace())
	}

	//s For a fake mirror make sure we update respective progress bars
	// and accounting readers under relevant conditions.
	if mj.isFake {
		if sURLs.SourceContent != nil {
			mj.status.Add(sURLs.SourceContent.Size)
		}
		mj.status.Update()
		return sURLs.WithError(nil)
	}

	sourceAlias := sURLs.SourceAlias
	sourceURL := sURLs.SourceContent.URL
	targetAlias := sURLs.TargetAlias
	targetURL := sURLs.TargetContent.URL
	length := sURLs.SourceContent.Size

	mj.status.SetCaption(sourceURL.String() + ": ")

	// Initialize target metadata.
	sURLs.TargetContent.Metadata = make(map[string]string)

	if mj.storageClass != "" {
		sURLs.TargetContent.Metadata["X-Amz-Storage-Class"] = mj.storageClass
	}

	if mj.multiMasterEnable {
		// Set multiMasterETagKey for the target.
		if sURLs.SourceContent.UserMetadata[multiMasterETagKey] == "" {
			sURLs.TargetContent.Metadata[multiMasterETagKey] = sURLs.SourceContent.ETag
		}

		if sURLs.SourceContent.UserMetadata[multiMasterSTagKey] == "" {
			sURLs.TargetContent.Metadata[multiMasterSTagKey] = mj.multiMasterSTag
		}
	}

	if mj.isPreserve {
		attrValue, pErr := getFileAttrMeta(sURLs, mj.encKeyDB)
		if pErr != nil {
			return sURLs.WithError(pErr)
		}
		if attrValue != "" {
			sURLs.TargetContent.Metadata["mc-attrs"] = attrValue
		}
	}

	// Initialize additional target user metadata.
	sURLs.TargetContent.UserMetadata = mj.userMetadata

	sourcePath := filepath.ToSlash(filepath.Join(sourceAlias, sourceURL.Path))
	targetPath := filepath.ToSlash(filepath.Join(targetAlias, targetURL.Path))
	mj.status.PrintMsg(mirrorMessage{
		Source:     sourcePath,
		Target:     targetPath,
		Size:       length,
		TotalCount: sURLs.TotalCount,
		TotalSize:  sURLs.TotalSize,
	})
	return uploadSourceToTargetURL(ctx, sURLs, mj.status, mj.encKeyDB)
}

// Update progress status
func (mj *mirrorJob) monitorMirrorStatus() (errDuringMirror bool) {
	// now we want to start the progress bar
	mj.status.Start()
	defer mj.status.Finish()

	for sURLs := range mj.statusCh {
		if sURLs.Error != nil {
			switch {
			case sURLs.SourceContent != nil:
				if !isErrIgnored(sURLs.Error) {
					errorIf(sURLs.Error.Trace(sURLs.SourceContent.URL.String()),
						fmt.Sprintf("Failed to copy `%s`.", sURLs.SourceContent.URL.String()))
					errDuringMirror = true
				}
			case sURLs.TargetContent != nil:
				// When sURLs.SourceContent is nil, we know that we have an error related to removing
				errorIf(sURLs.Error.Trace(sURLs.TargetContent.URL.String()),
					fmt.Sprintf("Failed to remove `%s`.", sURLs.TargetContent.URL.String()))
				errDuringMirror = true
			default:
				errorIf(sURLs.Error.Trace(), "Failed to perform mirroring.")
				errDuringMirror = true
			}
			if mj.multiMasterEnable {
				close(mj.stopCh)
				break
			}
		}

		if sURLs.SourceContent != nil {
		} else if sURLs.TargetContent != nil {
			// Construct user facing message and path.
			targetPath := filepath.ToSlash(filepath.Join(sURLs.TargetAlias, sURLs.TargetContent.URL.Path))
			size := sURLs.TargetContent.Size
			mj.status.PrintMsg(rmMessage{Key: targetPath, Size: size})
		}
	}

	return
}

// this goroutine will watch for notifications, and add modified objects to the queue
func (mj *mirrorJob) watchMirror(ctx context.Context, cancelMirror context.CancelFunc) {
	for {
		select {
		case event, ok := <-mj.watcher.Events():
			if !ok {
				return
			}

			// It will change the expanded alias back to the alias
			// again, by replacing the sourceUrlFull with the sourceAlias.
			// This url will be used to mirror.
			sourceAlias, sourceURLFull, _ := mustExpandAlias(mj.sourceURL)

			// If the passed source URL points to fs, fetch the absolute src path
			// to correctly calculate targetPath
			if sourceAlias == "" {
				tmpSrcURL, err := filepath.Abs(sourceURLFull)
				if err == nil {
					sourceURLFull = tmpSrcURL
				}
			}
			eventPath := event.Path
			if runtime.GOOS == "darwin" {
				// Strip the prefixes in the event path. Happens in darwin OS only
				eventPath = eventPath[strings.Index(eventPath, sourceURLFull):]
			}

			sourceURL := newClientURL(eventPath)

			// build target path, it is the relative of the eventPath with the sourceUrl
			// joined to the targetURL.
			sourceSuffix := strings.TrimPrefix(eventPath, sourceURLFull)
			//Skip the object, if it matches the Exclude options provided
			if matchExcludeOptions(mj.excludeOptions, sourceSuffix) {
				continue
			}

			targetPath := urlJoinPath(mj.targetURL, sourceSuffix)

			// newClient needs the unexpanded  path, newCLientURL needs the expanded path
			targetAlias, expandedTargetPath, _ := mustExpandAlias(targetPath)
			targetURL := newClientURL(expandedTargetPath)
			tgtSSE := getSSE(targetPath, mj.encKeyDB[targetAlias])

			if (event.Type == EventCreate) ||
				(event.Type == EventCreateCopy) ||
				(event.Type == EventCreatePutRetention) {
				mirrorURL := URLs{
					SourceAlias: sourceAlias,
					SourceContent: &clientContent{
						URL:       *sourceURL,
						Retention: event.Type == EventCreatePutRetention,
						Size:      event.Size,
						Metadata:  event.UserMetadata,
					},
					TargetAlias:   targetAlias,
					TargetContent: &clientContent{URL: *targetURL},
					encKeyDB:      mj.encKeyDB,
				}
				if mirrorURL.SourceContent.Metadata[multiMasterETagKey] != "" {
					// If source has multiMasterETagKey, it means that the
					// object was uplooaded by "mc mirror"
					// hence ignore the event to avoid copying it.
					continue
				}
				if mirrorURL.SourceContent.Size == 0 && mirrorURL.SourceContent.Retention {
					targetClient, err := newClient(targetPath)
					if err != nil {
						// cannot create targetclient
						mj.statusCh <- mirrorURL.WithError(err)
						return
					}
					shouldQueue := false
					if !mj.isOverwrite {
						_, err = targetClient.Stat(false, false, false, tgtSSE)
						if err == nil {
							continue
						} // doesn't exist
						shouldQueue = true
					}
					if shouldQueue || mj.isOverwrite || mj.multiMasterEnable {
						// adjust total, because we want to show progress of
						// the item still queued to be copied.
						mj.status.Add(mirrorURL.SourceContent.Size)
						mj.status.SetTotal(mj.status.Get()).Update()
						mj.status.AddCounts(1)
						mirrorURL.TotalSize = mj.status.Get()
						mirrorURL.TotalCount = mj.status.GetCounts()
						mj.statusCh <- mj.doMirror(ctx, cancelMirror, mirrorURL)
					}
					continue
				}
				shouldQueue := false
				if !mj.isOverwrite && !mj.multiMasterEnable {
					targetClient, err := newClient(targetPath)
					if err != nil {
						// cannot create targetclient
						mj.statusCh <- mirrorURL.WithError(err)
						return
					}
					_, err = targetClient.Stat(false, false, false, tgtSSE)
					if err == nil {
						if mirrorURL.SourceContent.Retention {
							shouldQueue = true
						} else {
							continue
						}
					} // doesn't exist
					shouldQueue = true
				}
				if shouldQueue || mj.isOverwrite || mj.multiMasterEnable {
					// adjust total, because we want to show progress
					// of the itemj stiil queued to be copied.
					mj.status.Add(mirrorURL.SourceContent.Size)
					mj.status.SetTotal(mj.status.Get()).Update()
					mj.status.AddCounts(1)
					mirrorURL.TotalSize = mj.status.Get()
					mirrorURL.TotalCount = mj.status.GetCounts()
					mj.statusCh <- mj.doMirror(ctx, cancelMirror, mirrorURL)
				}
			} else if event.Type == EventRemove {
				if strings.Contains(event.UserAgent, uaMirrorAppName) {
					continue
				}
				mirrorURL := URLs{
					SourceAlias:   sourceAlias,
					SourceContent: nil,
					TargetAlias:   targetAlias,
					TargetContent: &clientContent{URL: *targetURL},
					encKeyDB:      mj.encKeyDB,
				}
				mirrorURL.TotalCount = mj.status.GetCounts()
				mirrorURL.TotalSize = mj.status.Get()
				if mirrorURL.TargetContent != nil && (mj.isRemove || mj.multiMasterEnable) {
					mj.statusCh <- mj.doRemove(mirrorURL)
				}
			}

		case err := <-mj.watcher.Errors():
			switch err.ToGoError().(type) {
			case APINotImplemented:
				errorIf(err.Trace(),
					"Unable to Watch on source, perhaps source doesn't support Watching for events")
				return
			}
			mj.statusCh <- URLs{Error: err}
			return
		case <-globalContext.Done():
			return
		case <-mj.stopCh:
			return
		}
	}
}

func (mj *mirrorJob) watchURL(sourceClient Client) *probe.Error {
	return mj.watcher.Join(sourceClient, true)
}

// Fetch urls that need to be mirrored
func (mj *mirrorJob) startMirror(ctx context.Context, cancelMirror context.CancelFunc, stopParallel func()) {
	// Do not run multiple startMirror's
	mj.m.Lock()
	defer mj.m.Unlock()

	isMetadata := len(mj.userMetadata) > 0 || mj.isPreserve
	URLsCh := prepareMirrorURLs(mj.sourceURL, mj.targetURL, mj.isFake, mj.isOverwrite, mj.isRemove, isMetadata, mj.excludeOptions, mj.encKeyDB)

	for {
		select {
		case sURLs, ok := <-URLsCh:
			if !ok {
				if stopParallel != nil {
					stopParallel()
				}
				return
			}
			if sURLs.Error != nil {
				mj.statusCh <- sURLs
				continue
			}

			if sURLs.SourceContent != nil {
				if mj.olderThan != "" && isOlder(sURLs.SourceContent.Time, mj.olderThan) {
					continue
				}
				if mj.newerThan != "" && isNewer(sURLs.SourceContent.Time, mj.newerThan) {
					continue
				}
			}

			if sURLs.SourceContent != nil {
				mj.status.Add(sURLs.SourceContent.Size)
			}
			mj.status.SetTotal(mj.status.Get()).Update()
			mj.status.AddCounts(1)

			// Save total count.
			sURLs.TotalCount = mj.status.GetCounts()
			// Save totalSize.
			sURLs.TotalSize = mj.status.Get()

			if sURLs.SourceContent != nil {
				mj.queueCh <- func() URLs {
					return mj.doMirror(ctx, cancelMirror, sURLs)
				}
			} else if sURLs.TargetContent != nil && mj.isRemove {
				mj.queueCh <- func() URLs {
					return mj.doRemove(sURLs)
				}
			}
		case <-globalContext.Done():
			if stopParallel != nil {
				stopParallel()
			}
			cancelMirror()
			return
		case <-mj.stopCh:
			if stopParallel != nil {
				stopParallel()
			}
			cancelMirror()
			return
		}
	}
}

// when using a struct for copying, we could save a lot of passing of variables
func (mj *mirrorJob) mirror(ctx context.Context, cancelMirror context.CancelFunc) bool {

	var wg sync.WaitGroup

	// Starts watcher loop for watching for new events.
	if mj.isWatch {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mj.watchMirror(ctx, cancelMirror)
		}()
	}

	// Start mirroring.
	wg.Add(1)
	go func() {
		defer wg.Done()
		stopParallel := func() {
			close(mj.queueCh)
			mj.parallel.wait()
		}
		// startMirror locks and blocks itself.
		mj.startMirror(ctx, cancelMirror, stopParallel)
	}()

	// Close statusCh when both watch & mirror quits
	go func() {
		wg.Wait()
		close(mj.statusCh)
	}()

	return mj.monitorMirrorStatus()
}

func newMirrorJob(srcURL, dstURL string, isFake, isRemove, isOverwrite, isWatch, isPreserve, multiMasterEnable bool, excludeOptions []string, olderThan, newerThan string, storageClass string, multiMasterSTag string, userMetadata map[string]string, encKeyDB map[string][]prefixSSEPair) *mirrorJob {
	if multiMasterEnable {
		isPreserve = true
	}
	mj := mirrorJob{
		stopCh: make(chan struct{}),

		sourceURL: srcURL,
		targetURL: dstURL,

		isFake:            isFake,
		isRemove:          isRemove,
		isOverwrite:       isOverwrite,
		isWatch:           isWatch,
		isPreserve:        isPreserve,
		excludeOptions:    excludeOptions,
		olderThan:         olderThan,
		newerThan:         newerThan,
		storageClass:      storageClass,
		userMetadata:      userMetadata,
		encKeyDB:          encKeyDB,
		statusCh:          make(chan URLs),
		watcher:           NewWatcher(UTCNow()),
		multiMasterEnable: multiMasterEnable,
		multiMasterSTag:   multiMasterSTag,
	}

	mj.parallel, mj.queueCh = newParallelManager(mj.statusCh)

	// we'll define the status to use here,
	// do we want the quiet status? or the progressbar
	if globalQuiet {
		mj.status = NewQuietStatus(mj.parallel)
	} else if globalJSON {
		mj.status = NewQuietStatus(mj.parallel)
	} else {
		mj.status = NewProgressStatus(mj.parallel)
	}

	return &mj
}

// copyBucketPolicies - copy policies from source to dest
func copyBucketPolicies(srcClt, dstClt Client, isOverwrite bool) *probe.Error {
	rules, err := srcClt.GetAccessRules()
	if err != nil {
		switch err.ToGoError().(type) {
		case APINotImplemented:
			return nil
		}
		return err
	}
	// Set found rules to target bucket if permitted
	for _, r := range rules {
		originalRule, _, err := dstClt.GetAccess()
		if err != nil {
			return err
		}
		// Set rule only if it doesn't exist in the target bucket
		// or force flag is activated
		if originalRule == "none" || isOverwrite {
			err = dstClt.SetAccess(r, false)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// runMirror - mirrors all buckets to another S3 server
func runMirror(srcURL, dstURL string, ctx *cli.Context, encKeyDB map[string][]prefixSSEPair) bool {
	// This is kept for backward compatibility, `--force` means
	// --overwrite.
	isOverwrite := ctx.Bool("force")
	if !isOverwrite {
		isOverwrite = ctx.Bool("overwrite")
	}

	// Parse metadata.
	userMetaMap := make(map[string]string)
	if ctx.String("attr") != "" {
		var err *probe.Error
		userMetaMap, err = getMetaDataEntry(ctx.String("attr"))
		fatalIf(err, "Unable to parse attribute %v", ctx.String("attr"))
	}

	srcClt, err := newClient(srcURL)
	fatalIf(err, "Unable to initialize `"+srcURL+"`.")

	dstClt, err := newClient(dstURL)
	fatalIf(err, "Unable to initialize `"+dstURL+"`.")

	mirrorAllBuckets := (dstClt.GetURL().Type == objectStorage &&
		dstClt.GetURL().Path == string(dstClt.GetURL().Separator)) &&
		(srcClt.GetURL().Type == objectStorage &&
			srcClt.GetURL().Path == string(srcClt.GetURL().Separator))

	// Check if we are only trying to mirror one bucket from source.
	if dstClt.GetURL().Type == objectStorage &&
		dstClt.GetURL().Path == string(dstClt.GetURL().Separator) && !mirrorAllBuckets {
		dstURL = urlJoinPath(dstURL, srcClt.GetURL().Path)

		dstClt, err = newClient(dstURL)
		fatalIf(err, "Unable to initialize `"+dstURL+"`.")
	}

	multiMasterSTag := ctx.String("multi-master")
	multiMasterEnable := multiMasterSTag != ""

	// Create a new mirror job and execute it
	mj := newMirrorJob(srcURL, dstURL,
		ctx.Bool("fake"),
		ctx.Bool("remove"),
		isOverwrite,
		ctx.Bool("watch"),
		ctx.Bool("a"),
		multiMasterEnable,
		ctx.StringSlice("exclude"),
		ctx.String("older-than"),
		ctx.String("newer-than"),
		ctx.String("storage-class"),
		multiMasterSTag,
		userMetaMap,
		encKeyDB)

	go func() {
		<-globalContext.Done()
		os.Exit(globalErrorExitStatus)
	}()

	if mirrorAllBuckets {
		// Synchronize buckets using dirDifference function
		for d := range dirDifference(srcClt, dstClt, srcURL, dstURL) {
			if d.Error != nil {
				if mj.multiMasterEnable {
					errorIf(d.Error, "Failed to start mirroring.")
					return true
				}
				mj.status.fatalIf(d.Error, "Failed to start mirroring.")
			}
			if d.Diff == differInSecond {
				// Ignore buckets that only exist in target instance
				continue
			}

			sourceSuffix := strings.TrimPrefix(d.FirstURL, srcClt.GetURL().String())
			newSrcURL := path.Join(srcURL, sourceSuffix)
			newTgtURL := path.Join(dstURL, sourceSuffix)

			newSrcClt, _ := newClient(newSrcURL)
			newDstClt, _ := newClient(newTgtURL)

			if d.Diff == differInFirst {
				withLock := false
				mode, validity, unit, err := newSrcClt.GetObjectLockConfig()
				if err == nil {
					withLock = true
				}
				// Bucket only exists in the source, create the same bucket in the destination
				if err := newDstClt.MakeBucket(ctx.String("region"), false, withLock); err != nil {
					errorIf(err, "Unable to create bucket at `"+newTgtURL+"`.")
					continue
				}
				// object lock configuration set on bucket
				if mode != nil {
					errorIf(newDstClt.SetObjectLockConfig(mode, validity, unit),
						"Unable to set object lock config in `"+newTgtURL+"`.")
				}
				errorIf(copyBucketPolicies(newSrcClt, newDstClt, isOverwrite),
					"Unable to copy bucket policies to `"+newDstClt.GetURL().String()+"`.")
			}

			if mj.isWatch {
				// monitor mode will watch the source folders for changes,
				// and queue them for copying.
				if err := mj.watchURL(newSrcClt); err != nil {
					if mj.multiMasterEnable {
						errorIf(err, fmt.Sprintf("Failed to start monitoring."))
						return true
					}
					mj.status.fatalIf(err, fmt.Sprintf("Failed to start monitoring."))
				}
			}
		}
	} else {
		withLock := false
		mode, validity, unit, err := srcClt.GetObjectLockConfig()
		if err == nil {
			withLock = true
		}

		// Create bucket if it doesn't exist at destination.
		// ignore if already exists.
		if mj.multiMasterEnable {
			err = dstClt.MakeBucket(ctx.String("region"), true, withLock)
			errorIf(err, "Unable to create bucket at `"+dstURL+"`.")
			if err != nil {
				return true
			}
		} else {
			mj.status.fatalIf(dstClt.MakeBucket(ctx.String("region"), true, withLock),
				"Unable to create bucket at `"+dstURL+"`.")
		}

		// object lock configuration set on bucket
		if mode != nil {
			err = dstClt.SetObjectLockConfig(mode, validity, unit)
			errorIf(err, "Unable to set object lock config in `"+dstURL+"`.")
			if err != nil && mj.multiMasterEnable {
				return true
			}
		}

		err = copyBucketPolicies(srcClt, dstClt, isOverwrite)
		errorIf(err, "Unable to copy bucket policies to `"+dstClt.GetURL().String()+"`.")
		if err != nil && mj.multiMasterEnable {
			return true
		}
	}

	if !mirrorAllBuckets && mj.isWatch {
		// monitor mode will watch the source folders for changes,
		// and queue them for copying.
		if err := mj.watchURL(srcClt); err != nil {
			if mj.multiMasterEnable {
				errorIf(err, fmt.Sprintf("Failed to start monitoring."))
				return true
			}
			mj.status.fatalIf(err, fmt.Sprintf("Failed to start monitoring."))
		}
	}

	ctxt, cancelMirror := context.WithCancel(context.Background())
	defer cancelMirror()

	// Start mirroring job
	return mj.mirror(ctxt, cancelMirror)
}

// Main entry point for mirror command.
func mainMirror(ctx *cli.Context) error {
	// Parse encryption keys per command.
	encKeyDB, err := getEncKeys(ctx)
	fatalIf(err, "Unable to parse encryption keys.")

	// check 'mirror' cli arguments.
	checkMirrorSyntax(ctx, encKeyDB)

	// Additional command specific theme customization.
	console.SetColor("Mirror", color.New(color.FgGreen, color.Bold))

	args := ctx.Args()

	srcURL := args[0]
	tgtURL := args[1]

	if ctx.String("multi-master") != "" {
		for {
			runMirror(srcURL, tgtURL, ctx, encKeyDB)
			time.Sleep(time.Second * 2)
		}
	}

	if errorDetected := runMirror(srcURL, tgtURL, ctx, encKeyDB); errorDetected {
		return exitStatus(globalErrorExitStatus)
	}

	return nil
}
