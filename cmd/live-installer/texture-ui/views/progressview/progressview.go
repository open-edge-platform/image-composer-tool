// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package progressview

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"

	"github.com/open-edge-platform/image-composer-tool/cmd/live-installer/texture-ui/primitives/progressbar"
	"github.com/open-edge-platform/image-composer-tool/cmd/live-installer/texture-ui/uitext"
	"github.com/open-edge-platform/image-composer-tool/cmd/live-installer/texture-ui/uiutils"
	"github.com/open-edge-platform/image-composer-tool/internal/config"
	"github.com/open-edge-platform/image-composer-tool/internal/utils/logger"
)

// UI constants.
const (
	defaultPadding    = 1
	defaultProportion = 1
	defaultMoreDetail = false

	logTextHeight = 0
)

// ProgressView contains the Progress UI
type ProgressView struct {
	// UI elements
	app                 *tview.Application
	flex                *tview.Flex
	logText             *tview.TextView
	progressBar         *progressbar.ProgressBar
	centeredProgressBar *tview.Flex

	// Generate state
	alreadyShown bool
	moreDetails  bool

	// Callbacks
	performInstallation func(chan int, chan string)
	nextPage            func()
	quit                func()
}

var log = logger.Logger()

// New creates and returns a new ProgressView.
func New(performInstallation func(chan int, chan string)) *ProgressView {
	return &ProgressView{
		moreDetails:         defaultMoreDetail,
		performInstallation: performInstallation,
	}
}

// Initialize initializes the view.
func (pv *ProgressView) Initialize(backButtonText string, template *config.ImageTemplate, app *tview.Application, nextPage, previousPage, quit, refreshTitle func()) (err error) {
	pv.app = app
	pv.nextPage = nextPage
	pv.quit = quit

	pv.logText = tview.NewTextView().
		SetWordWrap(true).
		SetScrollable(false).
		SetDynamicColors(true).
		SetChangedFunc(func() {
			app.Draw()
		})

	pv.progressBar = progressbar.NewProgressBar().
		SetChangedFunc(func() {
			app.Draw()
		})

	pv.centeredProgressBar = uiutils.CenterVertically(pv.progressBar.GetHeight(), pv.progressBar)

	pv.flex = tview.NewFlex().SetDirection(tview.FlexRow)

	pv.switchDetailLevel(pv.moreDetails)

	// Box styling
	pv.logText.SetBorderPadding(defaultPadding, defaultPadding, defaultPadding, defaultPadding)

	pv.flex.SetBackgroundColor(tview.Styles.PrimitiveBackgroundColor)

	return
}

// HandleInput handles custom input.
func (pv *ProgressView) HandleInput(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	// Prevent exiting the UI here as installation has already begun.
	case tcell.KeyCtrlC:
		return nil
	case tcell.KeyCtrlA:
		pv.switchDetailLevel(!pv.moreDetails)
	}

	return event
}

// Reset resets the page, undoing any user input.
func (pv *ProgressView) Reset() (err error) {
	return
}

// Name returns the friendly name of the view.
func (pv *ProgressView) Name() string {
	return "PROGRESS"
}

// Title returns the title of the view.
func (pv *ProgressView) Title() string {
	return uitext.ProgressTitle
}

// Primitive returns the primary primitive to be rendered for the view.
func (pv *ProgressView) Primitive() tview.Primitive {
	return pv.flex
}

// OnShow gets called when the view is shown to the user.
func (pv *ProgressView) OnShow() {
	if pv.alreadyShown {
		log.Panicf("ProgressView shown more than once, unsupported behavior.")
	}

	pv.alreadyShown = true

	go pv.startInstallation()
}

func (pv *ProgressView) switchDetailLevel(moreDetail bool) {
	pv.moreDetails = moreDetail

	if moreDetail {
		pv.flex.RemoveItem(pv.centeredProgressBar)
		pv.flex.AddItem(pv.logText, logTextHeight, defaultProportion, true)
	} else {
		pv.flex.RemoveItem(pv.logText)
		pv.flex.AddItem(pv.centeredProgressBar, 0, defaultProportion, true)
	}
}

func (pv *ProgressView) startInstallation() {
	progress := make(chan int)
	status := make(chan string)

	wg := new(sync.WaitGroup)
	wg.Add(2)

	go pv.monitorProgress(progress, wg)
	go pv.monitorStatus(progress, status, wg)

	pv.performInstallation(progress, status)

	wg.Wait()

	pv.nextPage()
}

func (pv *ProgressView) monitorProgress(progress chan int, wg *sync.WaitGroup) {
	for progressUpdate := range progress {
		pv.progressBar.SetProgress(progressUpdate)
	}

	wg.Done()
}

func (pv *ProgressView) monitorStatus(progress chan int, status chan string, wg *sync.WaitGroup) {
	for statusUpdate := range status {
		if strings.Contains(statusUpdate, "provider initialized") {
			progress <- 10
		} else if strings.Contains(statusUpdate, "merged user and default configurations") {
			progress <- 20
		} else if strings.Contains(statusUpdate, "Building image") {
			progress <- 30
		} else if strings.Contains(statusUpdate, "Creating partition") {
			progress <- 40
		} else if strings.Contains(statusUpdate, "Installing OS for image") {
			progress <- 50
		} else if strings.Contains(statusUpdate, "Image system configuration") {
			progress <- 70
		} else if strings.Contains(statusUpdate, "Installing bootloader") {
			progress <- 80
		} else if strings.Contains(statusUpdate, "Image installation post-processing") {
			progress <- 90
		} else if strings.Contains(statusUpdate, "OS installation completed") {
			progress <- 100
		}
		pv.progressBar.SetStatus(statusUpdate)
		fmt.Fprintf(pv.logText, "%s", statusUpdate)
	}

	wg.Done()
}
