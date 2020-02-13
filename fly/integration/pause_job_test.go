package integration_test

import (
	"fmt"
	"net/http"
	"os/exec"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
	"github.com/onsi/gomega/ghttp"
)

var _ = FDescribe("Fly CLI", func() {
	Describe("Pause Job", func() {
		var (
			flyCmd       *exec.Cmd
			pipelineName string
			jobName      string
			fullJobName  string
			apiPath      string
		)

		BeforeEach(func() {
			pipelineName = "pipeline"
			jobName = "job-name-potato"
			fullJobName = fmt.Sprintf("%s/%s", pipelineName, jobName)
			apiPath = fmt.Sprintf("/api/v1/teams/main/pipelines/%s/jobs/%s/pause", pipelineName, jobName)

			flyCmd = exec.Command(flyPath, "-t", "some-target", "pause-job", "-j", fullJobName)
		})

		Context("when user is on the same team as the given pipeline/job's team", func() {
				BeforeEach(func() {
					atcServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("PUT", apiPath),
							ghttp.RespondWith(http.StatusOK, nil),
						),
					)
				})

				It("successfully pauses the job", func() {
					sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).NotTo(HaveOccurred())
					<-sess.Exited
					Expect(sess.ExitCode()).To(Equal(0))
					Eventually(sess.Out.Contents).Should(ContainSubstring(fmt.Sprintf("paused '%s'\n", jobName)))
				})
		})

		Context("user is NOT on the same team as the given pipeline/job's team", func() {
			BeforeEach(func() {
				atcServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("PUT", apiPath),
						ghttp.RespondWith(http.StatusForbidden, nil),
					),
				)
			})

			It("fails to pause the job", func() {
				sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				<-sess.Exited
				Expect(sess.ExitCode()).To(Equal(1))
				Eventually(sess.Err.Contents).Should(ContainSubstring("error"))
			})
		})

		Context("user is admin and NOT currently on the same team as the given pipeline/job", func() {
			BeforeEach(func() {
				apiPath = fmt.Sprintf("/api/v1/teams/other-team/pipelines/%s/jobs/%s/pause", pipelineName, jobName)

				adminAtcServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("PUT", apiPath),
						ghttp.RespondWith(http.StatusOK, nil),
					),
				)
			})

			It("successfully pauses the job", func() {
				Expect(func() {
					flyCmd = exec.Command(flyPath, "-t", "some-target", "pause-job", "-j", fullJobName, "--team", "other-team")
					sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).NotTo(HaveOccurred())
					<-sess.Exited
					Expect(sess.ExitCode()).To(Equal(0))
					Eventually(sess).Should(gbytes.Say(fmt.Sprintf("paused '%s'\n", jobName)))
				}).To(Change(func() int {
					return len(adminAtcServer.ReceivedRequests())
				}).By(2))
			})
		})

		Context("the pipeline/job does not exist", func() {
			BeforeEach(func() {
				adminAtcServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("PUT", "/api/v1/teams/main/pipelines/random-pipeline/jobs/random-job/pause"),
						ghttp.RespondWith(http.StatusNotFound, nil),
					),
				)
			})

			It("returns an error", func() {
				flyCmd = exec.Command(flyPath, "-t", targetName, "pause-job", "-j", "random-pipeline/random-job")

				sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				Eventually(sess.Err.Contents).Should(ContainSubstring(`random-pipeline/random-job not found on team random-team`))
				<-sess.Exited
				Expect(sess.ExitCode()).To(Equal(1))
			})
		})

		Context("when a job fails to be paused using the API", func() {
				BeforeEach(func() {
					adminAtcServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("PUT", apiPath),
							ghttp.RespondWith(http.StatusInternalServerError, nil),
						),
					)
				})

				It("exits 1 and outputs an error", func() {
					Expect(func() {
						sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
						Expect(err).NotTo(HaveOccurred())
						Eventually(sess.Err).Should(gbytes.Say(`error`))
						<-sess.Exited
						Expect(sess.ExitCode()).To(Equal(1))
					}).To(Change(func() int {
						return len(adminAtcServer.ReceivedRequests())
					}).By(2))
				})
			})
	})
})
