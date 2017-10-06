/*
 * Minio Client (C) 2014, 2015 Minio, Inc.
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
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/cheggaaa/pb"
	"github.com/fatih/color"
	"github.com/minio/cli"
	"github.com/minio/mc/pkg/console"
	"github.com/minio/mc/pkg/probe"
)

// cp command flags.
var (
	cpFlags = []cli.Flag{
		cli.BoolFlag{
			Name:  "recursive, r",
			Usage: "Copy recursively.",
		},
		cli.UintFlag{
			Name:  "parallel, p",
			Usage: "Number of copies in parallel",
			Value: 1,
		},
	}
)

// Copy command.
var cpCmd = cli.Command{
	Name:   "cp",
	Usage:  "Copy files and objects.",
	Action: mainCopy,
	Before: setGlobalsFromContext,
	Flags:  append(cpFlags, globalFlags...),
	CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} [FLAGS] SOURCE [SOURCE...] TARGET

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
ENVIRONMENT VARIABLES:
  MC_MULTIPART_THREADS: To set number of multipart threads. By default it is 4.

EXAMPLES:
   1. Copy a list of objects from local file system to Amazon S3 cloud storage.
      $ {{.HelpName}} Music/*.ogg s3/jukebox/

   2. Copy a folder recursively from Minio cloud storage to Amazon S3 cloud storage.
      $ {{.HelpName}} --recursive play/mybucket/burningman2011/ s3/mybucket/

   3. Copy a folder recursively from Minio cloud storage to Amazon S3 cloud storage in parallel.
      $ {{.HelpName}} --recursive --parallel 10 play/mybucket/burningman2011/ s3/mybucket/

   4. Copy multiple local folders recursively to Minio cloud storage.
      $ {{.HelpName}} --recursive backup/2014/ backup/2015/ play/archive/

   5. Copy a bucket recursively from aliased Amazon S3 cloud storage to local filesystem on Windows.
      $ {{.HelpName}} --recursive s3\documents\2014\ C:\Backups\2014

   6. Copy an object with name containing unicode characters to Amazon S3 cloud storage.
      $ {{.HelpName}} 本語 s3/andoria/

   7. Copy a local folder with space separated characters to Amazon S3 cloud storage.
      $ {{.HelpName}} --recursive 'workdir/documents/May 2014/' s3/miniocloud

`,
}

// copyMessage container for file copy messages
type copyMessage struct {
	Status     string `json:"status"`
	Source     string `json:"source"`
	Target     string `json:"target"`
	Size       int64  `json:"size"`
	TotalCount int64  `json:"totalCount"`
	TotalSize  int64  `json:"totalSize"`
}

// String colorized copy message
func (c copyMessage) String() string {
	return console.Colorize("Copy", fmt.Sprintf("`%s` -> `%s`", c.Source, c.Target))
}

// JSON jsonified copy message
func (c copyMessage) JSON() string {
	c.Status = "success"
	copyMessageBytes, e := json.Marshal(c)
	fatalIf(probe.NewError(e), "Failed to marshal copy message.")

	return string(copyMessageBytes)
}

// copyStatMessage container for copy accounting message
type copyStatMessage struct {
	Total       int64
	Transferred int64
	Speed       float64
}

// copyStatMessage copy accounting message
func (c copyStatMessage) String() string {
	speedBox := pb.Format(int64(c.Speed)).To(pb.U_BYTES).String()
	if speedBox == "" {
		speedBox = "0 MB"
	} else {
		speedBox = speedBox + "/s"
	}
	message := fmt.Sprintf("Total: %s, Transferred: %s, Speed: %s", pb.Format(c.Total).To(pb.U_BYTES),
		pb.Format(c.Transferred).To(pb.U_BYTES), speedBox)
	return message
}

// doCopy - Copy a singe file from source to destination
func doCopy(cpURLs URLs, progressReader *progressBar, accountingReader *accounter) URLs {
	if cpURLs.Error != nil {
		cpURLs.Error = cpURLs.Error.Trace()
		return cpURLs
	}

	if !globalQuiet && !globalJSON {
		progressReader = progressReader.SetCaption(cpURLs.SourceContent.URL.String() + ": ")
	}

	sourceAlias := cpURLs.SourceAlias
	sourceURL := cpURLs.SourceContent.URL
	targetAlias := cpURLs.TargetAlias
	targetURL := cpURLs.TargetContent.URL
	length := cpURLs.SourceContent.Size

	var progress io.Reader
	if globalQuiet || globalJSON {
		sourcePath := filepath.ToSlash(filepath.Join(sourceAlias, sourceURL.Path))
		targetPath := filepath.ToSlash(filepath.Join(targetAlias, targetURL.Path))
		printMsg(copyMessage{
			Source:     sourcePath,
			Target:     targetPath,
			Size:       length,
			TotalCount: cpURLs.TotalCount,
			TotalSize:  cpURLs.TotalSize,
		})
		// Proxy reader to accounting reader only during quiet mode.
		if globalQuiet || globalJSON {
			progress = accountingReader
		}
	} else {
		// Set up progress reader.
		progress = progressReader.ProgressBar
	}
	return uploadSourceToTargetURL(cpURLs, progress)
}

// doCopyFake - Perform a fake copy to update the progress bar appropriately.
func doCopyFake(cpURLs URLs, progressReader *progressBar) URLs {
	if !globalQuiet && !globalJSON {
		progressReader.ProgressBar.Add64(cpURLs.SourceContent.Size)
	}
	return cpURLs
}

// doPrepareCopyURLs scans the source URL and prepares a list of objects for copying.
func doPrepareCopyURLs(session *sessionV8, trapCh <-chan bool) {
	// Separate source and target. 'cp' can take only one target,
	// but any number of sources.
	sourceURLs := session.Header.CommandArgs[:len(session.Header.CommandArgs)-1]
	targetURL := session.Header.CommandArgs[len(session.Header.CommandArgs)-1] // Last one is target

	var totalBytes int64
	var totalObjects int64

	// Access recursive flag inside the session header.
	isRecursive := session.Header.CommandBoolFlags["recursive"]

	// Create a session data file to store the processed URLs.
	dataFP := session.NewDataWriter()

	var scanBar scanBarFunc
	if !globalQuiet && !globalJSON { // set up progress bar
		scanBar = scanBarFactory()
	}

	URLsCh := prepareCopyURLs(sourceURLs, targetURL, isRecursive)
	done := false
	for !done {
		select {
		case cpURLs, ok := <-URLsCh:
			if !ok { // Done with URL preparation
				done = true
				break
			}
			if cpURLs.Error != nil {
				// Print in new line and adjust to top so that we don't print over the ongoing scan bar
				if !globalQuiet && !globalJSON {
					console.Eraseline()
				}
				if strings.Contains(cpURLs.Error.ToGoError().Error(), " is a folder.") {
					errorIf(cpURLs.Error.Trace(), "Folder cannot be copied. Please use `...` suffix.")
				} else {
					errorIf(cpURLs.Error.Trace(), "Unable to prepare URL for copying.")
				}
				break
			}

			jsonData, e := json.Marshal(cpURLs)
			if e != nil {
				session.Delete()
				fatalIf(probe.NewError(e), "Unable to prepare URL for copying. Error in JSON marshaling.")
			}
			fmt.Fprintln(dataFP, string(jsonData))
			if !globalQuiet && !globalJSON {
				scanBar(cpURLs.SourceContent.URL.String())
			}

			totalBytes += cpURLs.SourceContent.Size
			totalObjects++
		case <-trapCh:
			// Print in new line and adjust to top so that we don't print over the ongoing scan bar
			if !globalQuiet && !globalJSON {
				console.Eraseline()
			}
			session.Delete() // If we are interrupted during the URL scanning, we drop the session.
			os.Exit(0)
		}
	}
	session.Header.TotalBytes = totalBytes
	session.Header.TotalObjects = totalObjects
	session.Save()
}

func doCopySession(session *sessionV8) error {
	trapCh := signalTrap(os.Interrupt, syscall.SIGTERM, syscall.SIGKILL)

	if !session.HasData() {
		doPrepareCopyURLs(session, trapCh)
	}

	// Enable accounting reader by default.
	accntReader := newAccounter(session.Header.TotalBytes)

	// Prepare URL scanner from session data file.
	urlScanner := bufio.NewScanner(session.NewDataReader())
	// isCopied returns true if an object has been already copied
	// or not. This is useful when we resume from a session.
	isCopied := isLastFactory(session.Header.LastCopied)

	// Number of parallel
	parallel := 1
	if session.Header.CommandIntFlags["parallel"] > 1 {
		parallel = session.Header.CommandIntFlags["parallel"]
	}
	// Queue of doCopy() operation.
	var queueCh = make(chan URLs, parallel)
	// The number of waiting jobs
	var waitGroup = &sync.WaitGroup{}

	// Enable progress bar reader only during default mode.
	var progressReader *progressBar
	if !globalQuiet && !globalJSON { // set up progress bar
		progressReader = newProgressBar(session.Header.TotalBytes)
	}

	// Wait on status of doCopy() operation.
	var statusCh = make(chan URLs)

	for i := 0; i < parallel; i++ {
		go func() {
			for {
				cpURLs, ok := <-queueCh
				if !ok {
					return
				}
				statusCh <- doCopy(cpURLs, progressReader, accntReader)
				waitGroup.Done()
			}
		}()
	}

	go func() {
		// Loop through all urls.
		for urlScanner.Scan() {
			var cpURLs URLs
			// Unmarshal copyURLs from each line.
			json.Unmarshal([]byte(urlScanner.Text()), &cpURLs)

			// Save total count.
			cpURLs.TotalCount = session.Header.TotalObjects

			// Save totalSize.
			cpURLs.TotalSize = session.Header.TotalBytes

			// Verify if previously copied, notify progress bar.
			if isCopied(cpURLs.SourceContent.URL.String()) {
				statusCh <- doCopyFake(cpURLs, progressReader)
			} else {
				waitGroup.Add(1)
				queueCh <- cpURLs
			}
		}

		// Waiting to complete jobs
		waitGroup.Wait()
		// Close
		close(queueCh)
		// URLs feeding finished
		close(statusCh)

	}()

	var retErr error

loop:
	for {
		select {
		case <-trapCh:
			// Receive interrupt notification.
			if !globalQuiet && !globalJSON {
				console.Eraseline()
			}
			session.CloseAndDie()
		case cpURLs, ok := <-statusCh:
			// Status channel is closed, we should return.
			if !ok {
				break loop
			}
			if cpURLs.Error == nil {
				session.Header.LastCopied = cpURLs.SourceContent.URL.String()
				session.Save()
			} else {

				// Set exit status for any copy error
				retErr = exitStatus(globalErrorExitStatus)

				// Print in new line and adjust to top so that we
				// don't print over the ongoing progress bar.
				if !globalQuiet && !globalJSON {
					console.Eraseline()
				}
				errorIf(cpURLs.Error.Trace(cpURLs.SourceContent.URL.String()),
					fmt.Sprintf("Failed to copy `%s`.", cpURLs.SourceContent.URL.String()))
				if isErrIgnored(cpURLs.Error) {
					continue loop
				}
				// For critical errors we should exit. Session
				// can be resumed after the user figures out
				// the  problem.
				session.CloseAndDie()
			}
		}
	}

	if !globalQuiet && !globalJSON {
		if progressReader.ProgressBar.Get() > 0 {
			progressReader.ProgressBar.Finish()
		}
	} else {
		if !globalJSON && globalQuiet {
			console.Println(console.Colorize("Copy", accntReader.Stat().String()))
		}
	}

	return retErr
}

// mainCopy is the entry point for cp command.
func mainCopy(ctx *cli.Context) error {

	// check 'copy' cli arguments.
	checkCopySyntax(ctx)

	// Additional command speific theme customization.
	console.SetColor("Copy", color.New(color.FgGreen, color.Bold))

	session := newSessionV8()
	session.Header.CommandType = "cp"
	session.Header.CommandBoolFlags["recursive"] = ctx.Bool("recursive")
	session.Header.CommandIntFlags["parallel"] = ctx.Int("parallel")

	var e error
	if session.Header.RootPath, e = os.Getwd(); e != nil {
		session.Delete()
		fatalIf(probe.NewError(e), "Unable to get current working folder.")
	}

	// extract URLs.
	session.Header.CommandArgs = ctx.Args()
	e = doCopySession(session)
	session.Delete()

	return e
}
