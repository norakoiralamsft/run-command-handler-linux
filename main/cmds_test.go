package main

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ahmetalpbalkan/go-httpbin"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func Test_commandsExist(t *testing.T) {
	// we expect these subcommands to be handled
	expect := []string{"install", "enable", "disable", "uninstall", "update"}
	for _, c := range expect {
		_, ok := cmds[c]
		if !ok {
			t.Fatalf("cmd '%s' is not handled", c)
		}
	}
}

func Test_commands_shouldReportStatus(t *testing.T) {
	// - certain extension invocations are supposed to write 'N.status' files and some do not.

	// these subcommands should NOT report status
	require.False(t, cmds["install"].shouldReportStatus, "install should not report status")
	require.False(t, cmds["uninstall"].shouldReportStatus, "uninstall should not report status")

	// these subcommands SHOULD report status
	require.True(t, cmds["enable"].shouldReportStatus, "enable should report status")
	require.True(t, cmds["disable"].shouldReportStatus, "disable should report status")
	require.True(t, cmds["update"].shouldReportStatus, "update should report status")
}

func Test_checkAndSaveSeqNum_fails(t *testing.T) {
	// pass in invalid seqnum format
	_, err := checkAndSaveSeqNum(log.NewNopLogger(), 0, "/non/existing/dir")
	require.NotNil(t, err)
	require.Contains(t, err.Error(), `failed to save sequence number`)
}

func Test_checkAndSaveSeqNum(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	fp := filepath.Join(dir, "seqnum")
	defer os.RemoveAll(dir)

	nop := log.NewNopLogger()

	// no sequence number, 0 comes in.
	shouldExit, err := checkAndSaveSeqNum(nop, 0, fp)
	require.Nil(t, err)
	require.False(t, shouldExit)

	// file=0, seq=0 comes in. (should exit)
	shouldExit, err = checkAndSaveSeqNum(nop, 0, fp)
	require.Nil(t, err)
	require.True(t, shouldExit)

	// file=0, seq=1 comes in.
	shouldExit, err = checkAndSaveSeqNum(nop, 1, fp)
	require.Nil(t, err)
	require.False(t, shouldExit)

	// file=1, seq=1 comes in. (should exit)
	shouldExit, err = checkAndSaveSeqNum(nop, 1, fp)
	require.Nil(t, err)
	require.True(t, shouldExit)

	// file=1, seq=0 comes in. (should exit)
	shouldExit, err = checkAndSaveSeqNum(nop, 1, fp)
	require.Nil(t, err)
	require.True(t, shouldExit)
}

func Test_runCmd_success(t *testing.T) {
	var script = "date"
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	err = runCmd(log.NewContext(log.NewNopLogger()), dir, "", &handlerSettings{
		publicSettings: publicSettings{Source: &scriptSource{Script: script}},
	})
	require.Nil(t, err, "command should run successfully")

	// check stdout stderr files
	_, err = os.Stat(filepath.Join(dir, "stdout"))
	require.Nil(t, err, "stdout should exist")
	_, err = os.Stat(filepath.Join(dir, "stderr"))
	require.Nil(t, err, "stderr should exist")

	// Check embedded script if saved to file
	_, err = os.Stat(filepath.Join(dir, "script.sh"))
	require.Nil(t, err, "script.sh should exist")
	content, err := ioutil.ReadFile(filepath.Join(dir, "script.sh"))
	require.Nil(t, err, "script.sh read failure")
	require.Equal(t, script, string(content))
}

func Test_runCmd_fail(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	err = runCmd(log.NewContext(log.NewNopLogger()), dir, "", &handlerSettings{
		publicSettings: publicSettings{Source: &scriptSource{Script: "non-existing-cmd"}},
	})
	require.NotNil(t, err, "command terminated with exit status")
	require.Contains(t, err.Error(), "failed to execute command")
}

func Test_downloadScriptUri(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	downloadedFilePath, err := downloadScript(log.NewContext(log.NewNopLogger()),
		dir,
		&handlerSettings{
			publicSettings: publicSettings{
				Source: &scriptSource{ScriptURI: srv.URL + "/bytes/10"},
			},
		})
	require.Nil(t, err)

	// check the downloaded file
	fp := filepath.Join(dir, "10")
	require.Equal(t, fp, downloadedFilePath)
	_, err = os.Stat(fp)
	require.Nil(t, err, "%s is missing from download dir", fp)
}

func Test_decodeScript(t *testing.T) {
	testSubject := "bHMK"
	s, info, err := decodeScript(testSubject)

	require.NoError(t, err)
	require.Equal(t, info, "4;3;gzip=0")
	require.Equal(t, s, "ls\n")
}

func Test_decodeScriptGzip(t *testing.T) {
	testSubject := "H4sIACD731kAA8sp5gIAfShLWgMAAAA="
	s, info, err := decodeScript(testSubject)

	require.NoError(t, err)
	require.Equal(t, info, "32;3;gzip=1")
	require.Equal(t, s, "ls\n")
}

func Test_downloadScriptUri_BySASFailsSucceedsByManagedIdentity(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	defer os.RemoveAll(dir)

	UseMockSASDownloadFailure = true
	handler := func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.RequestURI, "/samplecontainer/sample.sh?SASToken") {
			writer.WriteHeader(http.StatusOK) // Download successful using managed identity
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	_, err = downloadScript(log.NewContext(log.NewNopLogger()),
		dir,
		&handlerSettings{
			publicSettings: publicSettings{
				Source: &scriptSource{ScriptURI: srv.URL + "/samplecontainer/sample.sh?SASToken"},
			},
			protectedSettings: protectedSettings{
				SourceSASToken: "SASToken",
				SourceManagedIdentity: &RunCommandManagedIdentity{
					ClientId: "00b64c6a-6dbf-41e0-8707-74132d5cf53f",
				},
			},
		})
	require.Nil(t, err)
	UseMockSASDownloadFailure = false
}
