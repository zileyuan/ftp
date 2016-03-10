package ftp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// 链接信息结构体
type Connection struct {
	control  io.ReadWriteCloser
	hostname string
}

// 传输模式定义
var CRLF = "\r\n"
var ASCII = "A"
var BINARY = "I"
var IMAGE = "I"

// FTP服务器链接方法
func Dial(host string) (*Connection, error) {
	if host == "" {
		return nil, errors.New("FTP Connection Error: Host can not be blank!")
	}
	if !hasPort(host) {
		return nil, errors.New("FTP Connection Error: Host must have a port! e.g. host:21")
	}
	conn, err := net.Dial("tcp", host)
	if err != nil {
		return nil, err
	}

	welcomeMsg := make([]byte, 1024)
	_, err = conn.Read(welcomeMsg)
	if err != nil {
		return nil, errors.New("Couldn't read the server's initital connection information. Error: " + err.Error())
	}
	code, err := strconv.Atoi(string(welcomeMsg[0:3]))
	err = checkResponseCode(2, code)
	if err != nil {
		return nil, errors.New("Couldn't read the server's Welcome Message. Error: " + err.Error())
	}

	hostParts := strings.Split(host, ":")
	return &Connection{conn, hostParts[0]}, nil
}

// 发送命令到FTP服务器
func (c *Connection) Cmd(command string, arg string) (code int, response string, err error) {
	formattedCommand := command + " " + arg + CRLF

	_, err = c.control.Write([]byte(formattedCommand))
	if err != nil {
		return 0, "", err
	}

	// 解析 response
	reader := bufio.NewReader(c.control)
	regex := regexp.MustCompile("[0-9][0-9][0-9] ")
	for {
		ln, err := reader.ReadString('\n')
		if err != nil {
			return 0, "", err
		}

		response += ln
		if regex.MatchString(ln) {
			break
		}
	}
	code, err = strconv.Atoi(response[0:3])
	if err != nil {
		return 0, response, err
	}
	return code, response, err
}

// 登陆 FTP服务器
func (c *Connection) Login(user string, password string) error {
	if user == "" {
		return errors.New("FTP Connection Error: User can not be blank!")
	}
	if password == "" {
		return errors.New("FTP Connection Error: Password can not be blank!")
	}
	// TODO: Check the server's response codes.
	_, _, err := c.Cmd("USER", user)
	_, _, err = c.Cmd("PASS", password)
	if err != nil {
		return err
	}
	return nil
}

// 退出FTP服务器
func (c *Connection) Logout() error {
	_, _, err := c.Cmd("QUIT", "")
	if err != nil {
		return err
	}
	err = c.control.Close()
	if err != nil {
		return err
	}
	return nil
}

// 下载文件
func (c *Connection) DownloadFile(src, dest, mode string) error {
	// 使用被动模式
	pasvCode, pasvLine, err := c.Cmd("PASV", "")
	if err != nil {
		return err
	}
	pasvErr := checkResponseCode(2, int(pasvCode))
	if pasvErr != nil {
		msg := fmt.Sprintf("Cannot set PASV. Error: %v", pasvErr)
		return errors.New(msg)
	}
	dataPort, err := extractDataPort(pasvLine)
	if err != nil {
		return err
	}

	// 设置FTP传输模式
	typeCode, typeLine, err := c.Cmd("TYPE", mode)
	if err != nil {
		return err
	}
	typeErr := checkResponseCode(2, typeCode)
	if typeErr != nil {
		msg := fmt.Sprintf("Cannot set TYPE. Error: '%v'. Line: '%v'", typeErr, typeLine)
		return errors.New(msg)
	}

	command := []byte("RETR " + src + CRLF)
	_, err = c.control.Write(command)
	if err != nil {
		return err
	}

	// 根据PASV命令返回的结果创建链接用于数据传输
	remoteConnectString := c.hostname + ":" + fmt.Sprintf("%d", dataPort)
	fmt.Println(remoteConnectString)
	download_conn, err := net.Dial("tcp", remoteConnectString)
	defer download_conn.Close()
	if err != nil {
		msg := fmt.Sprintf("Couldn't connect to server's remote data port. Error: %v", err)
		return errors.New(msg)
	}

	// 创建本地文件
	var filePerms os.FileMode = 0664
	destFile, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY, filePerms)
	defer destFile.Close()
	if err != nil {
		msg := fmt.Sprintf("Cannot open destination file, '%s'. %v", dest, err)
		return errors.New(msg)
	}

	bufLen := 1024
	buf := make([]byte, bufLen)

	// 写入本地文件
	for {
		bytesRead, readErr := download_conn.Read(buf)
		if bytesRead > 0 {
			_, err := destFile.Write(buf[0:bytesRead])
			if err != nil {
				msg := fmt.Sprintf("Coudn't write to file, '%s'. Error: %v", dest, err)
				return errors.New(msg)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

// 上传文件
// 上传文件的立场和下载文件基本相同，只不过数据流相反
func (c *Connection) UploadFile(src, dest, mode string) error {
	pasvCode, pasvLine, err := c.Cmd("PASV", "")
	if err != nil {
		return err
	}
	pasvErr := checkResponseCode(2, pasvCode)
	if pasvErr != nil {
		msg := fmt.Sprintf("Cannot set PASV. Error: %v", pasvErr)
		return errors.New(msg)
	}
	dataPort, err := extractDataPort(pasvLine)
	if err != nil {
		return err
	}

	typeCode, typeLine, err := c.Cmd("TYPE", mode)
	if err != nil {
		return err
	}
	typeErr := checkResponseCode(2, typeCode)
	if typeErr != nil {
		msg := fmt.Sprintf("Cannot set TYPE. Error: '%v'. Line: '%v'", typeErr, typeLine)
		return errors.New(msg)
	}
	command := []byte("STOR " + dest + CRLF)
	_, err = c.control.Write(command)
	if err != nil {
		return err
	}

	remoteConnectString := c.hostname + ":" + fmt.Sprintf("%d", dataPort)
	upload_conn, err := net.Dial("tcp", remoteConnectString)
	defer upload_conn.Close()
	if err != nil {
		msg := fmt.Sprintf("Couldn't connect to server's remote data port. Error: %v", err)
		return errors.New(msg)
	}

	sourceFile, err := os.Open(src)
	defer sourceFile.Close()
	if err != nil {
		msg := fmt.Sprintf("Cannot open src file, '%s'. %v", src, err)
		errors.New(msg)
	}

	bufLen := 1024
	buf := make([]byte, bufLen)

	for {
		bytesRead, readErr := sourceFile.Read(buf)
		if bytesRead > 0 {
			_, writeErr := upload_conn.Write(buf[0:bytesRead])
			if writeErr != nil {
				msg := fmt.Sprintf("Coudn't write file to server, '%s'. Error: %v", sourceFile, writeErr)
				return errors.New(msg)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

// 检查相应码
func checkResponseCode(expectCode, code int) error {
	if 1 <= expectCode && expectCode < 10 && code/100 != expectCode ||
		10 <= expectCode && expectCode < 100 && code/10 != expectCode ||
		100 <= expectCode && expectCode < 1000 && code != expectCode {
		msg := fmt.Sprintf("Bad response from server. Expected: %d, Got: %d", expectCode, code)
		return errors.New(msg)
	}
	return nil
}

// 从字符串中解析出主机端口号
func extractDataPort(line string) (port int, err error) {
	portPattern := "[0-9]+,[0-9]+,[0-9]+,[0-9]+,([0-9]+,[0-9]+)"
	re, err := regexp.Compile(portPattern)
	if err != nil {
		return 0, err
	}
	match := re.FindStringSubmatch(line)
	if len(match) == 0 {
		msg := "Cannot find data port in server output: " + line
		return 0, errors.New(msg)
	}
	octets := strings.Split(match[1], ",")
	firstOctet, _ := strconv.Atoi(octets[0])
	secondOctet, _ := strconv.Atoi(octets[1])
	port = int(firstOctet*256) + secondOctet

	return port, nil
}

func hasPort(s string) bool {
	return strings.LastIndex(s, ":") > strings.LastIndex(s, "]")
}
