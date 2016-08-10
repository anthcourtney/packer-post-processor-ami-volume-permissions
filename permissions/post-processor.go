package permissions

import (
  "errors"
  "fmt"
  "regexp"
  "github.com/aws/aws-sdk-go/aws"
  "github.com/aws/aws-sdk-go/aws/session"
  "github.com/aws/aws-sdk-go/service/ec2"
  "github.com/mitchellh/packer/builder/amazon/common"
  "github.com/mitchellh/packer/helper/config"
  "github.com/mitchellh/packer/packer"
  "github.com/mitchellh/packer/template/interpolate"
)

// Config is the post-processor configuration with interpolation supported.
//
// Supports:
// * access_key
// * secret_key
// * region
// * skip_region_validation
// * token
// * profile
//
// See Specifying Amazon Credentials (https://www.packer.io/docs/builders/amazon.html) for details on these config
// parameters.
type Config struct {
  common.AccessConfig `mapstructure:",squash"`

  ctx interpolate.Context
}

// PostProcessor holds the Config object.
type PostProcessor struct {
  config Config
}

// Configure sets the Config object with configuration values from the Packer template.
func (p *PostProcessor) Configure(raws ...interface{}) error {

  err := config.Decode(&p.config, &config.DecodeOpts{
    Interpolate:        true,
    InterpolateContext: &p.config.ctx,
    InterpolateFilter: &interpolate.RenderFilter{
      Exclude: []string{},
    },
  }, raws...)

  if err != nil {
    return err
  }

  return nil
}

// PostProcess parses the AMI ID from the artifact ID, retrieves the launch permissions and block devices for the AMI.
// For each device that has an EBS snapshot it copies the users and groups of the launch permissions to the
// create volume permissions of the volume.
//
// AWS artifact ID output has the format of <region>:<ami_id>, for example: ap-southeast-2:ami-4f8fae2c
func (p *PostProcessor) PostProcess(ui packer.Ui, artifact packer.Artifact) (packer.Artifact, bool, error) {

	ui.Say(fmt.Sprintf("%s", artifact.String()))

	r, _ := regexp.Compile("ami-[a-z0-9]+")
	amiID := r.FindString(artifact.Id())
	if amiID == "" {
		return artifact, false, fmt.Errorf("could not find AMI ID in artifact id '%s'", artifact.Id())
	}

	ui.Say(fmt.Sprintf("AMI ID: %s", amiID))

	config, err := p.config.Config()
	if err != nil {
		return artifact, false, fmt.Errorf("could not create AWS config: %v", err)
	}

	session := session.New(config)
	ec2conn := ec2.New(session)

	imageAttributeOutput, err := ec2conn.DescribeImageAttribute(&ec2.DescribeImageAttributeInput{
		Attribute: aws.String(ec2.ImageAttributeNameLaunchPermission),
		ImageId:   aws.String(amiID),
	})
	if err != nil {
		return artifact, false, fmt.Errorf("could not get image launch permission attribute for image %s: %v", amiID, err)
	}
	amiPermissions := imageAttributeOutput.LaunchPermissions
	ui.Say(fmt.Sprintf("AMI permissions: %v", amiPermissions))

	// Cannot call DescribeImageAttribute to retreive the block device mappings since we'll get the following error when
	// we do: AuthFailure: Unauthorized attempt to access restricted resource
	// Documented workaround is to run DescribeImages() instead
	imagesOutput, err := ec2conn.DescribeImages(&ec2.DescribeImagesInput{
		ImageIds: []*string{aws.String(amiID)},
	})
	if err != nil {
		return artifact, false, fmt.Errorf("could not get image block device mapping attribute for image %s: %v", amiID, err)
	}

	if err := p.fixSnapshotsForImages(ui, imagesOutput.Images, ec2conn, amiPermissions); err != nil {
		return artifact, false, err
	}

	return artifact, true, nil
}

func (p *PostProcessor) fixSnapshotsForImages(ui packer.Ui, images []*ec2.Image, ec2conn *ec2.EC2, amiPermissions []*ec2.LaunchPermission) error {
	foundSnapshotDevice := false
	for _, image := range images {
		for _, device := range image.BlockDeviceMappings {
			ui.Say(fmt.Sprintf("Checking device %s", aws.StringValue(device.DeviceName)))
			if device.Ebs != nil {
				if device.Ebs.SnapshotId != nil {
					foundSnapshotDevice = true
					snapshotID := aws.StringValue(device.Ebs.SnapshotId)
					if err := p.fixSnapshotPermissions(ui, ec2conn, snapshotID, amiPermissions); err != nil {
						return err
					}
				}
			}
		}
	}

	if !foundSnapshotDevice {
		return errors.New("Did not find any devices with EBS snapshots")
	}

	return nil
}

func (p *PostProcessor) fixSnapshotPermissions(ui packer.Ui, ec2conn *ec2.EC2, snapshotID string, amiPermissions []*ec2.LaunchPermission) error {
	ui.Say(fmt.Sprintf("Snapshot ID: %s", snapshotID))

	snapshotPermissions := []*ec2.CreateVolumePermission{}
	for _, amiPermission := range amiPermissions {
		snapshotPermissions = append(snapshotPermissions, &ec2.CreateVolumePermission{Group: amiPermission.Group, UserId: amiPermission.UserId})
	}

	ui.Say(fmt.Sprintf("Snapshot Permissions: %v", snapshotPermissions))

	_, err := ec2conn.ModifySnapshotAttribute(&ec2.ModifySnapshotAttributeInput{
		SnapshotId: aws.String(snapshotID),
		CreateVolumePermission: &ec2.CreateVolumePermissionModifications{
			Add: snapshotPermissions,
		}})
	if err != nil {
		return fmt.Errorf("could not modify snapshot attributes: %v", err)
	}

	return nil
}
