package iamlivecore

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"github.com/iann0036/goproxy"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"math/big"
	"time"
)

func CreateCertificates(ctx context.Context, dnsNames, secretName, secretNamespace string) error {
	conf, err := rest.InClusterConfig()

	if err != nil {
		return fmt.Errorf("unable to create k8s config: %w", err)
	}

	k8s := kubernetes.NewForConfigOrDie(conf)

	secretExists, err := loadCertificateAuthority(ctx, k8s, secretName, secretNamespace)

	if err != nil {
		return fmt.Errorf("unable to check if certificate exists: %w", err)
	}

	if secretExists {
		fmt.Println("certificate already exists - no action required")
		return nil
	}

	fmt.Println("certificate does not exist - creating")
	caPEM, caPrivateKeyPEM, err := createCertificateAuthority()

	if err != nil {
		return fmt.Errorf("unable to create certificate authority: %w", err)
	}

	err = storeCertificateSecret(ctx, k8s, caPEM, caPrivateKeyPEM, secretName, secretNamespace)

	if err != nil {
		return fmt.Errorf("unable to store certificate secret: %w", err)
	}

	_, err = loadCertificateAuthority(ctx, k8s, secretName, secretNamespace)

	return nil
}

func loadCertificateAuthority(
	ctx context.Context,
	k8s *kubernetes.Clientset,
	secretName string,
	secretNamespace string,
) (bool, error) {
	secret, err := k8s.CoreV1().Secrets(secretNamespace).Get(ctx, secretName, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("unable to get certificate authority secret: %w", err)
	}

	certificate := secret.Data["ca.crt"]
	privateKey := secret.Data["ca.key"]

	ca, err := tls.X509KeyPair(certificate, privateKey)

	if err != nil {
		return false, fmt.Errorf("unable to create goproxy CA: %w", err)
	}

	if ca.Leaf, err = x509.ParseCertificate(ca.Certificate[0]); err != nil {
		return false, fmt.Errorf("unable to parse goproxy CA certificate: %w", err)
	}

	goproxy.GoproxyCa = ca
	goproxy.OkConnect = &goproxy.ConnectAction{Action: goproxy.ConnectAccept, TLSConfig: goproxy.TLSConfigFromCA(&ca)}
	goproxy.MitmConnect = &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: goproxy.TLSConfigFromCA(&ca)}
	goproxy.HTTPMitmConnect = &goproxy.ConnectAction{Action: goproxy.ConnectHTTPMitm, TLSConfig: goproxy.TLSConfigFromCA(&ca)}
	goproxy.RejectConnect = &goproxy.ConnectAction{Action: goproxy.ConnectReject, TLSConfig: goproxy.TLSConfigFromCA(&ca)}

	fmt.Println("Loaded certificate authority")

	return true, nil
}
func createCertificateAuthority() (*bytes.Buffer, *bytes.Buffer, error) {
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(2019),
		Subject: pkix.Name{
			Organization:  []string{"Otterize, Inc"},
			Country:       []string{"US"},
			Province:      []string{"Delaware"},
			Locality:      []string{"Dover"},
			StreetAddress: []string{"850 New Burton Road, Suite 201"},
			PostalCode:    []string{"19904"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	caPrivateKey, err := rsa.GenerateKey(rand.Reader, 4096)

	if err != nil {
		return nil, nil, fmt.Errorf("unable to generate CA private key: %w", err)
	}

	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caPrivateKey.PublicKey, caPrivateKey)

	if err != nil {
		return nil, nil, fmt.Errorf("unable to create CA certificate: %w", err)
	}

	caPEM := new(bytes.Buffer)
	err = pem.Encode(caPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	})

	if err != nil {
		return nil, nil, fmt.Errorf("unable to encode CA certificate: %w", err)
	}

	caPrivateKeyPEM := new(bytes.Buffer)
	err = pem.Encode(caPrivateKeyPEM, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(caPrivateKey),
	})

	if err != nil {
		return nil, nil, fmt.Errorf("unable to encode CA private key: %w", err)
	}

	fmt.Println("Created certificate authority")

	return caPEM, caPrivateKeyPEM, nil
}

func storeCertificateSecret(
	ctx context.Context,
	k8s *kubernetes.Clientset,
	caPEM *bytes.Buffer,
	caPrivateKeyPEM *bytes.Buffer,
	secretName string,
	secretNamespace string,
) error {
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: secretNamespace,
		},
		Data: map[string][]byte{
			"ca.crt": caPEM.Bytes(),
			"ca.key": caPrivateKeyPEM.Bytes(),
		},
	}

	_, err := k8s.CoreV1().Secrets(secretNamespace).Create(ctx, &secret, metav1.CreateOptions{})

	if err != nil {
		return fmt.Errorf("unable to store certificate authority secret: %w", err)
	}

	fmt.Println("Stored certificate secret")
	return nil
}
