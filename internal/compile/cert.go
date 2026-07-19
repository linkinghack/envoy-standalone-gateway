package compile

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

// certVerifier 抽象证书文件检查（本质是 IO），便于测试注入假实现，
// 保持 link() 主体可测（SD5：编译器主体不依赖具体 IO 实现）。
type certVerifier struct {
	// verifyKeyPair 检查 cert/key 文件存在、可解析且公钥配对（openssl 语义）。
	verifyKeyPair func(certFile, keyFile string) error
	// verifyCAFile 检查 CA 文件存在且至少含一个可解析的 PEM 证书。
	verifyCAFile func(caFile string) error
}

// defaultCertVerifier 返回基于标准库 crypto/tls + crypto/x509 的默认实现
// （不 exec openssl）。
func defaultCertVerifier() certVerifier {
	return certVerifier{
		verifyKeyPair: defaultVerifyKeyPair,
		verifyCAFile:  defaultVerifyCAFile,
	}
}

// defaultVerifyKeyPair 用 tls.LoadX509KeyPair 完成存在性、PEM 解析与
// cert/key 公钥配对校验（"private key does not match public key"）。
func defaultVerifyKeyPair(certFile, keyFile string) error {
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		return err
	}
	return nil
}

// defaultVerifyCAFile 检查 CA 文件存在且至少含一个可解析的 PEM 证书。
func defaultVerifyCAFile(caFile string) error {
	data, err := os.ReadFile(caFile)
	if err != nil {
		return err
	}
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return fmt.Errorf("invalid CA certificate in %s: %w", caFile, err)
		}
		return nil
	}
	return errors.New("no PEM certificate found in " + caFile)
}
