package docker_test

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/cloudway/platform/container"
	"github.com/cloudway/platform/pkg/manifest"
)

var _ = Describe("Create Container", func() {
	const NAMESPACE = "container_create_test"

	var (
		ctx        = context.Background()
		plugin     *manifest.Plugin
		options    container.CreateOptions
		containers []container.Container
		err        error
	)

	BeforeEach(func() {
		plugin, err = pluginHub.GetPluginInfo("mock")
		Expect(err).NotTo(HaveOccurred())

		options = container.CreateOptions{
			Name:      "test",
			Namespace: NAMESPACE,
			Plugin:    plugin,
			Scaling:   1,
		}

		containers = nil
		err = nil
	})

	AfterEach(func() {
		for _, c := range containers {
			Expect(c.Destroy(ctx)).To(Succeed())
		}
	})

	It("should success with correct options", func() {
		containers, err = dockerCli.Create(ctx, options)
		Expect(err).NotTo(HaveOccurred())
		Expect(containers).To(HaveLen(1))
	})

	It("should fail if no name specified", func() {
		options.Name = ""
		containers, err = dockerCli.Create(ctx, options)
		Expect(err).To(HaveOccurred())
	})

	It("should fail if no namespace specified", func() {
		options.Namespace = ""
		containers, err = dockerCli.Create(ctx, options)
		Expect(err).To(HaveOccurred())
	})

	It("should fail if no plugin specified", func() {
		Expect(func() {
			options.Plugin = nil
			dockerCli.Create(ctx, options)
		}).To(Panic())
	})

	It("should fail to create multiple service containers", func() {
		service, err := pluginHub.GetPluginInfo("mockdb")
		Expect(err).NotTo(HaveOccurred())
		options.Plugin = service

		containers, err = dockerCli.Create(ctx, options)
		Expect(err).NotTo(HaveOccurred())

		more, err := dockerCli.Create(ctx, options)
		Expect(err).To(HaveOccurred())

		containers = append(containers, more...)
	})

	It("should success to create multiple service containers with different name", func() {
		service, err := pluginHub.GetPluginInfo("mockdb")
		Expect(err).NotTo(HaveOccurred())
		options.Plugin = service

		options.ServiceName = "db1"
		containers, err = dockerCli.Create(ctx, options)
		Expect(err).NotTo(HaveOccurred())

		options.ServiceName = "db2"
		more, err := dockerCli.Create(ctx, options)
		Expect(err).NotTo(HaveOccurred())

		containers = append(containers, more...)
	})

	Context("Scaling", func() {
		It("should fail if container exceeding maximum scaling level", func() {
			containers, err = dockerCli.Create(ctx, options)
			Expect(err).NotTo(HaveOccurred())
			Expect(containers).To(HaveLen(1))

			_, err = dockerCli.Create(ctx, options)
			Expect(err).To(HaveOccurred())
		})

		It("should success when scaling up containers", func() {
			containers, err = dockerCli.Create(ctx, options)
			Expect(err).NotTo(HaveOccurred())
			Expect(containers).To(HaveLen(1))

			options.Scaling = 3
			more, err := dockerCli.Create(ctx, options)
			Expect(err).NotTo(HaveOccurred())
			Expect(more).To(HaveLen(2))

			containers = append(containers, more...)
		})

		It("should fail when scaling down containers (scaling down is handled by broker)", func() {
			options.Scaling = 3
			containers, err = dockerCli.Create(ctx, options)
			Expect(err).NotTo(HaveOccurred())
			Expect(containers).To(HaveLen(3))

			options.Scaling = 2
			_, err = dockerCli.Create(ctx, options)
			Expect(err).To(HaveOccurred())
		})

		It("should fail when specify zero scaling value", func() {
			options.Scaling = 0
			containers, err = dockerCli.Create(ctx, options)
			Expect(err).To(HaveOccurred())
		})

		It("should fail when specify negative scaling value", func() {
			options.Scaling = -2
			containers, err = dockerCli.Create(ctx, options)
			Expect(err).To(HaveOccurred())
		})
	})
})
