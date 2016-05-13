package commands_test

import (
	"errors"

	"github.com/pivotal-cf-experimental/bosh-bootloader/aws"
	"github.com/pivotal-cf-experimental/bosh-bootloader/aws/iam"
	"github.com/pivotal-cf-experimental/bosh-bootloader/commands"
	"github.com/pivotal-cf-experimental/bosh-bootloader/fakes"
	"github.com/pivotal-cf-experimental/bosh-bootloader/storage"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Update LBs", func() {
	var (
		command                   commands.UpdateLBs
		certificateManager        *fakes.CertificateManager
		availabilityZoneRetriever *fakes.AvailabilityZoneRetriever
		infrastructureManager     *fakes.InfrastructureManager
		clientProvider            *fakes.ClientProvider
		iamClient                 *fakes.IAMClient
		cloudFormationClient      *fakes.CloudFormationClient
		ec2Client                 *fakes.EC2Client
	)

	var updateLBs = func(certificatePath string, keyPath string, state storage.State) (storage.State, error) {
		return command.Execute(commands.GlobalFlags{
			EndpointOverride: "some-endpoint",
		}, []string{
			"--cert", certificatePath,
			"--key", keyPath,
		}, state)
	}

	BeforeEach(func() {
		certificateManager = &fakes.CertificateManager{}
		clientProvider = &fakes.ClientProvider{}
		availabilityZoneRetriever = &fakes.AvailabilityZoneRetriever{}
		infrastructureManager = &fakes.InfrastructureManager{}

		iamClient = &fakes.IAMClient{}
		cloudFormationClient = &fakes.CloudFormationClient{}
		ec2Client = &fakes.EC2Client{}

		clientProvider.IAMClientCall.Returns.Client = iamClient
		clientProvider.CloudFormationClientCall.Returns.Client = cloudFormationClient
		clientProvider.EC2ClientCall.Returns.Client = ec2Client

		availabilityZoneRetriever.RetrieveCall.Returns.AZs = []string{"a", "b", "c"}
		certificateManager.CreateCall.Returns.CertificateName = "some-certificate-name"
		certificateManager.DescribeCall.Returns.Certificate = iam.Certificate{
			ARN: "some-certificate-arn",
		}

		command = commands.NewUpdateLBs(certificateManager, clientProvider, availabilityZoneRetriever, infrastructureManager)
	})

	Describe("Execute", func() {
		It("creates the new certificate and private key", func() {
			updateLBs("some-cert.crt", "some-key.key", storage.State{
				AWS: storage.AWS{
					AccessKeyID:     "some-access-key-id",
					SecretAccessKey: "some-secret-access-key",
					Region:          "some-region",
				},
			})

			Expect(clientProvider.IAMClientCall.Receives.Config).To(Equal(aws.Config{
				AccessKeyID:      "some-access-key-id",
				SecretAccessKey:  "some-secret-access-key",
				Region:           "some-region",
				EndpointOverride: "some-endpoint",
			}))

			Expect(certificateManager.CreateCall.Receives.Client).To(Equal(iamClient))
			Expect(certificateManager.CreateCall.Receives.Certificate).To(Equal("some-cert.crt"))
			Expect(certificateManager.CreateCall.Receives.PrivateKey).To(Equal("some-key.key"))
		})

		It("updates cloudformation with the new certificate", func() {
			updateLBs("some-cert.crt", "some-key.key", storage.State{
				Stack: storage.Stack{
					Name:   "some-stack",
					LBType: "some-lb-type",
				},
				AWS: storage.AWS{
					AccessKeyID:     "some-access-key-id",
					SecretAccessKey: "some-secret-access-key",
					Region:          "some-region",
				},
				KeyPair: storage.KeyPair{
					Name: "some-key-pair",
				},
			})

			Expect(clientProvider.EC2ClientCall.Receives.Config).To(Equal(aws.Config{
				AccessKeyID:      "some-access-key-id",
				SecretAccessKey:  "some-secret-access-key",
				Region:           "some-region",
				EndpointOverride: "some-endpoint",
			}))

			Expect(clientProvider.CloudFormationClientCall.Receives.Config).To(Equal(aws.Config{
				AccessKeyID:      "some-access-key-id",
				SecretAccessKey:  "some-secret-access-key",
				Region:           "some-region",
				EndpointOverride: "some-endpoint",
			}))

			Expect(availabilityZoneRetriever.RetrieveCall.Receives.Region).To(Equal("some-region"))
			Expect(availabilityZoneRetriever.RetrieveCall.Receives.EC2Client).To(Equal(ec2Client))

			Expect(certificateManager.DescribeCall.Receives.CertificateName).To(Equal("some-certificate-name"))
			Expect(certificateManager.DescribeCall.Receives.IAMClient).To(Equal(iamClient))

			Expect(infrastructureManager.UpdateCall.Receives.KeyPairName).To(Equal("some-key-pair"))
			Expect(infrastructureManager.UpdateCall.Receives.NumberOfAvailabilityZones).To(Equal(3))
			Expect(infrastructureManager.UpdateCall.Receives.StackName).To(Equal("some-stack"))
			Expect(infrastructureManager.UpdateCall.Receives.LBType).To(Equal("some-lb-type"))
			Expect(infrastructureManager.UpdateCall.Receives.LBCertificateARN).To(Equal("some-certificate-arn"))
			Expect(infrastructureManager.UpdateCall.Receives.CloudFormationClient).To(Equal(cloudFormationClient))
		})

		It("deletes the existing certificate and private key", func() {
			updateLBs("some-cert.crt", "some-key.key", storage.State{
				CertificateName: "some-certificate-name",
			})

			Expect(certificateManager.DeleteCall.Receives.IAMClient).To(Equal(iamClient))
			Expect(certificateManager.DeleteCall.Receives.CertificateName).To(Equal("some-certificate-name"))
		})

		Describe("state manipulation", func() {
			It("updates the state with the new certificate name", func() {
				certificateManager.CreateCall.Returns.CertificateName = "some-new-certificate-name"

				state, err := updateLBs("some-cert.crt", "some-key.key", storage.State{
					CertificateName: "some-certificate-name",
				})
				Expect(err).NotTo(HaveOccurred())

				Expect(state.CertificateName).To(Equal("some-new-certificate-name"))
			})
		})

		Describe("failure cases", func() {
			It("returns an error when invalid flags are provided", func() {
				_, err := command.Execute(commands.GlobalFlags{}, []string{
					"--invalid-flag",
				}, storage.State{})

				Expect(err).To(MatchError(ContainSubstring("flag provided but not defined")))
			})

			It("returns an error when the cloudformation client cannot be constructed", func() {
				clientProvider.CloudFormationClientCall.Returns.Error = errors.New("cloudformation client construction failed")
				_, err := updateLBs("some-cert.crt", "some-key.key", storage.State{})
				Expect(err).To(MatchError("cloudformation client construction failed"))
			})

			It("returns an error when the ec2 client cannot be constructed", func() {
				clientProvider.EC2ClientCall.Returns.Error = errors.New("ec2 client construction failed")
				_, err := updateLBs("some-cert.crt", "some-key.key", storage.State{})
				Expect(err).To(MatchError("ec2 client construction failed"))
			})

			It("returns an error when the IAM client cannot be constructed", func() {
				clientProvider.IAMClientCall.Returns.Error = errors.New("iam client construction failed")
				_, err := updateLBs("some-cert.crt", "some-key.key", storage.State{})
				Expect(err).To(MatchError("iam client construction failed"))
			})

			It("returns an error when infrastructure update fails", func() {
				infrastructureManager.UpdateCall.Returns.Error = errors.New("failed to update stack")
				_, err := updateLBs("some-cert.crt", "some-key.key", storage.State{})
				Expect(err).To(MatchError("failed to update stack"))
			})

			It("returns an error when certificate describer fails", func() {
				certificateManager.DescribeCall.Returns.Error = errors.New("certificate failed to descript")
				_, err := updateLBs("some-cert.crt", "some-key.key", storage.State{})
				Expect(err).To(MatchError("certificate failed to descript"))
			})

			It("returns an error when availability zone retriever fails", func() {
				availabilityZoneRetriever.RetrieveCall.Returns.Error = errors.New("az retrieve failed")
				_, err := updateLBs("some-cert.crt", "some-key.key", storage.State{})
				Expect(err).To(MatchError("az retrieve failed"))
			})

			It("returns an error when certificate creation fails", func() {
				certificateManager.CreateCall.Returns.Error = errors.New("certificate creation failed")
				_, err := updateLBs("some-cert.crt", "some-key.key", storage.State{})
				Expect(err).To(MatchError("certificate creation failed"))
			})

			It("returns an error when certificate deletion fails", func() {
				certificateManager.DeleteCall.Returns.Error = errors.New("certificate deletion failed")
				_, err := updateLBs("some-cert.crt", "some-key.key", storage.State{})
				Expect(err).To(MatchError("certificate deletion failed"))
			})
		})
	})
})