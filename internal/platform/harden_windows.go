package platform

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// werFaultReportingFlagNoHeap: a crash report carries no heap contents.
	werFaultReportingFlagNoHeap = 1
	// processImageLoadPolicy is PROCESS_MITIGATION_POLICY ProcessImageLoadPolicy.
	processImageLoadPolicy = 10
	// Bits of PROCESS_MITIGATION_IMAGE_LOAD_POLICY.Flags.
	imageLoadNoRemoteImages            = 0x1
	imageLoadNoLowMandatoryLabelImages = 0x2
)

// Harden lowers the chance of a memory dump through Windows Error Reporting
// and forbids loading images from remote shares and low-integrity locations.
// Policies that WebView2 does not tolerate (dynamic code, strict handle check,
// signature, extension points) stay off. Both measures are tried; the errors
// of failed ones are joined.
func Harden() error {
	var errs []error

	kernel32 := windows.NewLazySystemDLL("kernel32.dll")

	// The Go runtime has already set WER_FAULT_REPORTING_NO_UI; keep its flags.
	var flags uint32

	getFlags := kernel32.NewProc("WerGetFlags")
	if hr, _, _ := getFlags.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&flags))); int32(hr) < 0 {
		errs = append(errs, fmt.Errorf("WerGetFlags: HRESULT 0x%08x", uint32(hr)))
	}

	setFlags := kernel32.NewProc("WerSetFlags")
	if hr, _, _ := setFlags.Call(uintptr(flags | werFaultReportingFlagNoHeap)); int32(hr) < 0 {
		errs = append(errs, fmt.Errorf("WerSetFlags: HRESULT 0x%08x", uint32(hr)))
	}

	policy := uint32(imageLoadNoRemoteImages | imageLoadNoLowMandatoryLabelImages)

	setPolicy := kernel32.NewProc("SetProcessMitigationPolicy")
	if ok, _, err := setPolicy.Call(
		processImageLoadPolicy,
		uintptr(unsafe.Pointer(&policy)),
		unsafe.Sizeof(policy),
	); ok == 0 {
		errs = append(errs, fmt.Errorf("SetProcessMitigationPolicy ProcessImageLoadPolicy: %w", err))
	}

	return errors.Join(errs...)
}
