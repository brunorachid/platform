package container

import (
    "io"
    "fmt"
    "bytes"
    "strings"
    "golang.org/x/net/context"
    "github.com/docker/engine-api/types"
)

// StatusError reports an unsuccessful exit by a command
type StatusError struct {
    Code    int
    Message string
}

func (e StatusError) Error() string {
    return fmt.Sprintf("%s, Code: %d", e.Message, e.Code)
}

// Execute command in application container.
func (c *Container) Exec(user string, stdin io.Reader, stdout, stderr io.Writer, cmd ...string) error {
    cli, err := docker_client()
    if err != nil {
        return err
    }

    execConfig := types.ExecConfig{
        User:           user,
        Tty:            false,
        AttachStdin:    stdin != nil,
        AttachStdout:   true,
        AttachStderr:   true,
        Cmd:            cmd,
    }

    ctx := context.Background()

    execResp, err := cli.ContainerExecCreate(ctx, c.ID, execConfig)
    if err != nil {
        return err
    }
    execId := execResp.ID

    resp, err := cli.ContainerExecAttach(ctx, execId, execConfig)
    if err != nil {
        return err
    }
    defer resp.Close()

    err = pumpStreams(ctx, stdin, stdout, stderr, resp)
    if err != nil {
        return err
    }

    inspectResp, err := cli.ContainerExecInspect(ctx, execId)
    if err != nil {
        return err
    } else if inspectResp.ExitCode != 0 {
        return StatusError{Code: inspectResp.ExitCode}
    } else {
        return nil
    }
}

func pumpStreams(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, resp types.HijackedResponse) error {
    var err error

    receiveStdout := make(chan error, 1)
    if stdout != nil || stderr != nil {
        go func() {
            _, err = stdCopy(stdout, stderr, resp.Reader)
            receiveStdout <- err
        }()
    }

    stdinDone := make(chan struct{})
    go func() {
        if stdin != nil {
            io.Copy(resp.Conn, stdin)
        }
        resp.CloseWrite()
        close(stdinDone)
    }()

    select {
    case err := <-receiveStdout:
        if err != nil {
            return err
        }
    case <-stdinDone:
        if stdout != nil || stderr != nil {
            select {
            case err := <-receiveStdout:
                if err != nil {
                    return err
                }
            case <-ctx.Done():
            }
        }
    case <-ctx.Done():
    }

    return nil
}

// Execute the command and accumulate error messages from standard error of
// the command.
func (c *Container) ExecE(user string, in io.Reader, out io.Writer, cmd ...string) error {
    var errbuf bytes.Buffer
    err := c.Exec(user, in, out, &errbuf, cmd...)
    if se, ok := err.(StatusError); ok && se.Message == "" {
        err = StatusError{Message: chomp(&errbuf), Code: se.Code}
    }
    return err
}

// Silently execute the command and accumulate error messages from standard
// error of the command.
//
// Equivalent to: ExecE(user, nil, nil, cmd...)
func (c *Container) ExecQ(user string, cmd ...string) error {
    return c.ExecE(user, nil, nil, cmd...)
}

// Performs the expansion by executing command and return the contents
// as the standard output of the command, with any trailing newlines
// deleted.
func (c *Container) Subst(user string, in io.Reader, cmd ...string) (string, error) {
    var outbuf, errbuf bytes.Buffer
    err := c.Exec(user, in, &outbuf, &errbuf, cmd...)
    if se, ok := err.(StatusError); ok && se.Message == "" {
        err = StatusError{Message: chomp(&errbuf), Code: se.Code}
    }
    return chomp(&outbuf), err
}

func chomp(b *bytes.Buffer) string {
    return strings.TrimRight(b.String(), "\r\n")
}