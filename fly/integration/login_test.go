package integration_test

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"regexp"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
	"github.com/onsi/gomega/ghttp"

	"github.com/concourse/concourse/atc"
	"github.com/concourse/concourse/fly/version"
)

var _ = Describe("login Command", func() {
	var (
		adminAtcServer *ghttp.Server
		tmpDir         string
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = ioutil.TempDir("", "fly-test")
		Expect(err).ToNot(HaveOccurred())

		os.Setenv("HOME", tmpDir)
	})

	AfterEach(func() {
		os.RemoveAll(tmpDir)
	})

	Describe("login with no target name", func() {
		var (
			flyCmd *exec.Cmd
		)

		BeforeEach(func() {
			adminAtcServer = ghttp.NewServer()
			adminAtcServer.AppendHandlers(
				infoHandler(),
			)
			flyCmd = exec.Command(flyPath, "login", "-c", adminAtcServer.URL())
		})

		AfterEach(func() {
			adminAtcServer.Close()
		})

		It("instructs the user to specify --target", func() {
			sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())

			<-sess.Exited
			Expect(sess.ExitCode()).To(Equal(1))

			Expect(sess.Err).To(gbytes.Say(`name for the target must be specified \(--target/-t\)`))
		})
	})

	Context("with no team name", func() {
		BeforeEach(func() {
			adminAtcServer = ghttp.NewServer()
		})

		AfterEach(func() {
			adminAtcServer.Close()
		})

		It("falls back to atc.DefaultTeamName team", func() {
			adminAtcServer.AppendHandlers(
				infoHandler(),
				tokenHandler(),
			)

			flyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL(), "-u", "user", "-p", "pass")

			sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())

			Eventually(sess).Should(gbytes.Say("logging in to team 'main'"))

			<-sess.Exited
			Expect(sess.ExitCode()).To(Equal(0))
		})

		Context("when already logged in as different team", func() {
			BeforeEach(func() {
				adminAtcServer.AppendHandlers(
					infoHandler(),
					tokenHandler(),
				)

				setupFlyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL(), "-n", "some-team", "-u", "user", "-p", "pass")
				err := setupFlyCmd.Run()
				Expect(err).NotTo(HaveOccurred())
			})

			It("uses the saved team name", func() {
				adminAtcServer.AppendHandlers(
					infoHandler(),
					tokenHandler(),
				)

				flyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-u", "user", "-p", "pass")
				sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				Eventually(sess).Should(gbytes.Say("logging in to team 'some-team'"))

				<-sess.Exited
				Expect(sess.ExitCode()).To(Equal(0))
			})
		})
	})

	Context("with no specified flag but extra arguments ", func() {

		BeforeEach(func() {
			adminAtcServer = ghttp.NewServer()
		})

		AfterEach(func() {
			adminAtcServer.Close()
		})

		It("return error indicating login failed with unknown arguments", func() {

			flyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL(), "unknown-argument", "blah")

			sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())

			<-sess.Exited
			Expect(sess.ExitCode()).NotTo(Equal(0))
			Expect(sess.Err).To(gbytes.Say(`unexpected argument \[unknown-argument, blah\]`))
		})
	})

	Context("with a team name", func() {
		flyLogin := func() *gexec.Session {
			flyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL(), "-n", "some-team", "-u", "dummy-user", "-p", "dummy-pass")

			sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())

			Eventually(sess).Should(gbytes.Say("logging in to team 'some-team'"))

			return sess
		}

		BeforeEach(func() {
			adminAtcServer = ghttp.NewServer()
		})

		AfterEach(func() {
			adminAtcServer.Close()
		})

		It("uses specified team", func() {
			adminAtcServer.AppendHandlers(
				infoHandler(),
				tokenHandler(),
			)

			flyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL(), "-n", "some-team", "-u", "user", "-p", "pass")

			sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())

			Eventually(sess).Should(gbytes.Say("logging in to team 'some-team'"))

			<-sess.Exited
			Expect(sess.ExitCode()).To(Equal(0))
		})

		Context("when the token is given", func() {
			var encodedString string
			var existedTeamName string = "some-team"
			JustBeforeEach(func() {
				Expect(encodedString).NotTo(Equal(""))
				encodedToken := base64.StdEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(encodedString))
				adminAtcServer.AppendHandlers(
					infoHandler(),
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", "/sky/token"),
						ghttp.RespondWithJSONEncoded(
							200,
							map[string]string{
								"token_type":   "Bearer",
								"access_token": "foo." + encodedToken,
							},
						),
					),
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/api/v1/teams"),
						ghttp.RespondWithJSONEncoded(200, []atc.Team{
							atc.Team{
								ID:   1,
								Name: existedTeamName,
							},
						},
						),
					),
				)
			})
			Context("when the token's roles doesn't include the team", func() {
				BeforeEach(func() {
					encodedString = `{
					"teams": {
						"some-other-team": ["owner"]
					},
					"user_id": "test",
					"user_name": "test"
				}`

				})

				It("fails", func() {
					sess := flyLogin()
					<-sess.Exited
					Expect(sess.ExitCode()).To(Equal(1))
					Expect(string(sess.Err.Contents())).To(ContainSubstring("user [test] is not in team [some-team]"))
				})
			})

			Context("when the token's roles doesn't include the team, but the user is admin", func() {
				BeforeEach(func() {
					encodedString = `{
					"teams": {
						"some-other-team": ["owner"]
					},
					"user_id": "test",
					"user_name": "test",
						"is_admin": true
					}`
				})
				Context("the team does exist", func() {
					It("success", func() {
						sess := flyLogin()
						<-sess.Exited
						Expect(sess.ExitCode()).To(Equal(0))
						Expect(sess.Out).Should(gbytes.Say("target saved"))
					})
					BeforeEach(func() {
						existedTeamName = "some-team"
					})
				})

				Context("the team does NOT exist", func() {
					It("fails", func() {
						sess := flyLogin()
						<-sess.Exited
						Expect(sess.ExitCode()).NotTo(Equal(0))
						Eventually(sess.Err).Should(gbytes.Say("error: team some-team doesn't exist"))
					})
					BeforeEach(func() {
						existedTeamName = "some-other-existed-team"
					})
				})

			})

			Context("when the token's (legacy) team list doesn't include the team", func() {
				BeforeEach(func() {
					encodedString = `{
					"teams": ["some-other-team"],
					"user_id": "test",
					"user_name": "test"
				}`
				})

				It("fails", func() {
					flyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL(), "-n", "some-team", "-u", "dummy-user", "-p", "dummy-pass")

					sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).NotTo(HaveOccurred())

					Eventually(sess).Should(gbytes.Say("logging in to team 'some-team'"))

					<-sess.Exited
					Expect(sess.ExitCode()).To(Equal(1))
					Expect(sess.Err.Contents()).To(ContainSubstring("user [test] is not in team [some-team]"))
				})
			})

		})

		Context("when tracing is not enabled", func() {
			It("does not print out API calls", func() {
				adminAtcServer.AppendHandlers(
					infoHandler(),
					tokenHandler(),
				)

				flyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL(), "-n", "some-team", "-u", "user", "-p", "pass")

				sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())

				Consistently(sess.Err).ShouldNot(gbytes.Say("HTTP/1.1 200 OK"))
				Consistently(sess.Out).ShouldNot(gbytes.Say("HTTP/1.1 200 OK"))

				<-sess.Exited
				Expect(sess.ExitCode()).To(Equal(0))
			})
		})

		Context("when tracing is enabled", func() {
			It("prints out API calls", func() {
				adminAtcServer.AppendHandlers(
					infoHandler(),
					tokenHandler(),
				)

				flyCmd := exec.Command(flyPath, "--verbose", "-t", "some-target", "login", "-c", adminAtcServer.URL(), "-n", "some-team", "-u", "user", "-p", "pass")

				sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())

				Eventually(sess.Err).Should(gbytes.Say("HTTP/1.1 200 OK"))

				<-sess.Exited
				Expect(sess.ExitCode()).To(Equal(0))
			})
		})

		Context("when already logged in as different team", func() {
			BeforeEach(func() {
				adminAtcServer.AppendHandlers(
					infoHandler(),
					tokenHandler(),
				)

				setupFlyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL(), "-n", "some-team", "-u", "user", "-p", "pass")
				err := setupFlyCmd.Run()
				Expect(err).NotTo(HaveOccurred())
			})

			It("passes provided team name", func() {
				adminAtcServer.AppendHandlers(
					infoHandler(),
					tokenHandler(),
				)

				flyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-n", "some-other-team", "-u", "user", "-p", "pass")

				sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())

				<-sess.Exited
				Expect(sess.ExitCode()).To(Equal(0))
			})
		})
	})

	Describe("with ca cert", func() {
		BeforeEach(func() {
			adminAtcServer = ghttp.NewUnstartedServer()
			cert, err := tls.X509KeyPair([]byte(serverCert), []byte(serverKey))
			Expect(err).NotTo(HaveOccurred())

			adminAtcServer.HTTPTestServer.TLS = &tls.Config{
				Certificates: []tls.Certificate{cert},
			}
			adminAtcServer.HTTPTestServer.StartTLS()
		})

		AfterEach(func() {
			adminAtcServer.Close()
		})

		Context("when already logged in with ca cert", func() {
			var caCertFilePath string

			BeforeEach(func() {
				adminAtcServer.AppendHandlers(
					infoHandler(),
					tokenHandler(),
				)

				caCertFile, err := ioutil.TempFile("", "fly-login-test")
				Expect(err).NotTo(HaveOccurred())
				caCertFilePath = caCertFile.Name()

				err = ioutil.WriteFile(caCertFilePath, []byte(serverCert), os.ModePerm)
				Expect(err).NotTo(HaveOccurred())

				setupFlyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL(), "-n", "some-team", "--ca-cert", caCertFilePath, "-u", "user", "-p", "pass")

				sess, err := gexec.Start(setupFlyCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				<-sess.Exited
				Expect(sess.ExitCode()).To(Equal(0))
			})

			AfterEach(func() {
				os.RemoveAll(caCertFilePath)
			})

			Context("when ca cert is not provided", func() {
				It("is using saved ca cert", func() {
					adminAtcServer.AppendHandlers(
						infoHandler(),
						tokenHandler(),
					)

					flyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-n", "some-team", "-u", "user", "-p", "pass")

					sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).NotTo(HaveOccurred())

					<-sess.Exited
					Expect(sess.ExitCode()).To(Equal(0))
				})
			})
		})
	})

	Describe("login", func() {
		var (
			flyCmd *exec.Cmd
		)

		BeforeEach(func() {
			adminAtcServer = ghttp.NewServer()
		})

		AfterEach(func() {
			adminAtcServer.Close()
		})

		Context("with authorization_code grant", func() {
			BeforeEach(func() {
				adminAtcServer.AppendHandlers(
					infoHandler(),
				)
			})

			It("instructs the user to visit the top-level login endpoint with fly port", func() {
				flyCmd = exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL())

				stdin, err := flyCmd.StdinPipe()
				Expect(err).NotTo(HaveOccurred())

				sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())

				Eventually(sess.Out).Should(gbytes.Say("navigate to the following URL in your browser:"))
				Eventually(sess.Out).Should(gbytes.Say("http://127.0.0.1:(\\d+)/login\\?fly_port=(\\d+)"))
				Eventually(sess.Out).Should(gbytes.Say("or enter token manually"))

				_, err = fmt.Fprintf(stdin, "Bearer some-token\n")
				Expect(err).NotTo(HaveOccurred())

				err = stdin.Close()
				Expect(err).NotTo(HaveOccurred())

				<-sess.Exited
				Expect(sess.ExitCode()).To(Equal(0))
			})

			Context("token callback listener", func() {
				var resp *http.Response
				var req *http.Request
				var sess *gexec.Session

				BeforeEach(func() {
					flyCmd = exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL())
					_, err := flyCmd.StdinPipe()
					Expect(err).NotTo(HaveOccurred())
					sess, err = gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).NotTo(HaveOccurred())
					Eventually(sess.Out).Should(gbytes.Say("or enter token manually"))
					scanner := bufio.NewScanner(bytes.NewBuffer(sess.Out.Contents()))
					var match []string
					for scanner.Scan() {
						re := regexp.MustCompile("fly_port=(\\d+)")
						match = re.FindStringSubmatch(scanner.Text())
						if len(match) > 0 {
							break
						}
					}
					flyPort := match[1]
					listenerURL := fmt.Sprintf("http://127.0.0.1:%s?token=Bearer%%20some-token", flyPort)
					req, err = http.NewRequest("GET", listenerURL, nil)
					Expect(err).NotTo(HaveOccurred())
				})

				JustBeforeEach(func() {
					adminAtcServer.AppendHandlers(ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", "/fly_success"),
						ghttp.RespondWith(200, ""),
					))
					client := &http.Client{
						CheckRedirect: func(req *http.Request, via []*http.Request) error {
							return http.ErrUseLastResponse
						},
					}
					var err error
					resp, err = client.Do(req)
					Expect(err).NotTo(HaveOccurred())
					<-sess.Exited
					Expect(sess.ExitCode()).To(Equal(0))
				})

				It("sets a CORS header for the ATC being logged in to", func() {
					corsHeader := resp.Header.Get("Access-Control-Allow-Origin")
					Expect(corsHeader).To(Equal(adminAtcServer.URL()))
				})

				It("responds successfully", func() {
					Expect(resp.StatusCode).To(Equal(http.StatusOK))
				})

				Context("when the request comes from a human operating a browser", func() {
					BeforeEach(func() {
						req.Header.Add("Upgrade-Insecure-Requests", "1")
					})

					It("redirects back to noop fly success page", func() {
						Expect(resp.StatusCode).To(Equal(http.StatusFound))
						locationHeader := resp.Header.Get("Location")
						Expect(locationHeader).To(Equal(fmt.Sprintf("%s/fly_success?noop=true", adminAtcServer.URL())))
					})
				})
			})
		})

		Context("with password grant", func() {
			BeforeEach(func() {
				credentials := base64.StdEncoding.EncodeToString([]byte("fly:Zmx5"))
				adminAtcServer.AppendHandlers(
					infoHandler(),
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("POST", "/sky/token"),
						ghttp.VerifyHeaderKV("Content-Type", "application/x-www-form-urlencoded"),
						ghttp.VerifyHeaderKV("Authorization", fmt.Sprintf("Basic %s", credentials)),
						ghttp.VerifyFormKV("grant_type", "password"),
						ghttp.VerifyFormKV("username", "some_username"),
						ghttp.VerifyFormKV("password", "some_password"),
						ghttp.VerifyFormKV("scope", "openid profile email federated:id groups"),
						ghttp.RespondWithJSONEncoded(200, map[string]string{
							"token_type":   "Bearer",
							"access_token": "some-token",
						}),
					),
				)
			})

			It("takes username and password as cli arguments", func() {
				flyCmd = exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL(), "-u", "some_username", "-p", "some_password")
				sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())

				Consistently(sess.Out.Contents).ShouldNot(ContainSubstring("some_password"))

				Eventually(sess.Out).Should(gbytes.Say("target saved"))

				<-sess.Exited
				Expect(sess.ExitCode()).To(Equal(0))
			})

			Context("after logging in succeeds", func() {
				BeforeEach(func() {
					flyCmd = exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL(), "-u", "some_username", "-p", "some_password")
					sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).NotTo(HaveOccurred())

					Consistently(sess.Out.Contents).ShouldNot(ContainSubstring("some_password"))

					Eventually(sess.Out).Should(gbytes.Say("target saved"))

					<-sess.Exited
					Expect(sess.ExitCode()).To(Equal(0))
				})

				It("flyrc is backwards-compatible with pre-v5.4.0", func() {
					flyRcContents, err := ioutil.ReadFile(homeDir + "/.flyrc")
					Expect(err).NotTo(HaveOccurred())
					Expect(string(flyRcContents)).To(HavePrefix("targets:"))
				})

				Describe("running other commands", func() {
					BeforeEach(func() {
						adminAtcServer.AppendHandlers(
							infoHandler(),
							ghttp.CombineHandlers(
								ghttp.VerifyRequest("GET", "/api/v1/teams/main/pipelines"),
								ghttp.VerifyHeaderKV("Authorization", "Bearer some-token"),
								ghttp.RespondWithJSONEncoded(200, []atc.Pipeline{
									{Name: "pipeline-1"},
								}),
							),
						)
					})

					It("uses the saved token", func() {
						otherCmd := exec.Command(flyPath, "-t", "some-target", "pipelines")

						sess, err := gexec.Start(otherCmd, GinkgoWriter, GinkgoWriter)
						Expect(err).NotTo(HaveOccurred())

						<-sess.Exited

						Expect(sess).To(gbytes.Say("pipeline-1"))

						Expect(sess.ExitCode()).To(Equal(0))
					})
				})

				Describe("logging in again with the same target", func() {
					BeforeEach(func() {
						credentials := base64.StdEncoding.EncodeToString([]byte("fly:Zmx5"))

						adminAtcServer.AppendHandlers(
							infoHandler(),
							ghttp.CombineHandlers(
								ghttp.VerifyRequest("POST", "/sky/token"),
								ghttp.VerifyHeaderKV("Content-Type", "application/x-www-form-urlencoded"),
								ghttp.VerifyHeaderKV("Authorization", fmt.Sprintf("Basic %s", credentials)),
								ghttp.VerifyFormKV("grant_type", "password"),
								ghttp.VerifyFormKV("username", "some_other_user"),
								ghttp.VerifyFormKV("password", "some_other_pass"),
								ghttp.VerifyFormKV("scope", "openid profile email federated:id groups"),
								ghttp.RespondWithJSONEncoded(200, map[string]string{
									"token_type":   "Bearer",
									"access_token": "some-new-token",
								}),
							),
							infoHandler(),
							ghttp.CombineHandlers(
								ghttp.VerifyRequest("GET", "/api/v1/teams/main/pipelines"),
								ghttp.VerifyHeaderKV("Authorization", "Bearer some-new-token"),
								ghttp.RespondWithJSONEncoded(200, []atc.Pipeline{
									{Name: "pipeline-2"},
								}),
							),
						)
					})

					It("updates the token", func() {
						loginAgainCmd := exec.Command(flyPath, "-t", "some-target", "login", "-u", "some_other_user", "-p", "some_other_pass")

						sess, err := gexec.Start(loginAgainCmd, GinkgoWriter, GinkgoWriter)
						Expect(err).NotTo(HaveOccurred())

						Consistently(sess.Out.Contents).ShouldNot(ContainSubstring("some_other_pass"))

						Eventually(sess.Out).Should(gbytes.Say("target saved"))

						<-sess.Exited
						Expect(sess.ExitCode()).To(Equal(0))

						otherCmd := exec.Command(flyPath, "-t", "some-target", "pipelines")

						sess, err = gexec.Start(otherCmd, GinkgoWriter, GinkgoWriter)
						Expect(err).NotTo(HaveOccurred())

						<-sess.Exited

						Expect(sess).To(gbytes.Say("pipeline-2"))

						Expect(sess.ExitCode()).To(Equal(0))
					})
				})
			})
		})

		Context("when fly and atc differ in major versions", func() {
			var flyVersion string

			BeforeEach(func() {
				major, minor, patch, err := version.GetSemver(atcVersion)
				Expect(err).NotTo(HaveOccurred())

				flyVersion = fmt.Sprintf("%d.%d.%d", major+1, minor, patch)
				flyPath, err := gexec.Build(
					"github.com/concourse/concourse/fly",
					"-ldflags", fmt.Sprintf("-X github.com/concourse/concourse.Version=%s", flyVersion),
				)
				Expect(err).NotTo(HaveOccurred())
				flyCmd = exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL(), "-u", "user", "-p", "pass")

				adminAtcServer.AppendHandlers(
					infoHandler(),
					tokenHandler(),
				)
			})

			It("warns user and does not fail", func() {
				sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())

				Eventually(sess).Should(gexec.Exit(0))
				Expect(sess.Err).To(gbytes.Say(`fly version \(%s\) is out of sync with the target \(%s\). to sync up, run the following:\n\n    `, flyVersion, atcVersion))
				Expect(sess.Err).To(gbytes.Say(`fly.* -t some-target sync\n`))
			})
		})
	})

	Context("Super Admin", func() {
		var encodedString string
		var teams []atc.Team
		credentials := base64.StdEncoding.EncodeToString([]byte("fly:Zmx5"))
		var teamHandler = func(teams []atc.Team) http.HandlerFunc {
			return ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/api/v1/teams"),
				ghttp.VerifyHeaderKV("Authorization", "Bearer foo."+encodedString),
				ghttp.RespondWithJSONEncoded(200, teams),
			)
		}
		var adminTokenHandler = func() http.HandlerFunc {
			return ghttp.CombineHandlers(
				ghttp.VerifyRequest("POST", "/sky/token"),
				ghttp.VerifyHeaderKV("Content-Type", "application/x-www-form-urlencoded"),
				ghttp.VerifyHeaderKV("Authorization", fmt.Sprintf("Basic %s", credentials)),
				ghttp.VerifyFormKV("grant_type", "password"),
				ghttp.VerifyFormKV("username", "user"),
				ghttp.VerifyFormKV("password", "pass"),
				ghttp.VerifyFormKV("scope", "openid profile email federated:id groups"),
				ghttp.RespondWithJSONEncoded(200, map[string]string{
					"token_type":   "Bearer",
					"access_token": "foo." + encodedString,
				}),
			)
		}

		BeforeEach(func() {
			encodedString = base64.StdEncoding.EncodeToString([]byte(`{
					"teams": {
						"main": ["owner"]
					},
					"user_id": "test",
					"is_admin": true,
					"user_name": "test"
			}`))

			teams = []atc.Team{
				atc.Team{
					ID:   1,
					Name: "main",
				},
				atc.Team{
					ID:   2,
					Name: "other-team",
				},
			}

			adminAtcServer = ghttp.NewServer()
			adminAtcServer.AppendHandlers(
				infoHandler(),
				adminTokenHandler(),
				teamHandler(teams),
			)

			flyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL(), "-n", "main", "-u", "user", "-p", "pass")

			sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
			Expect(err).NotTo(HaveOccurred())

			Eventually(sess).Should(gbytes.Say("logging in to team 'main'"))

			<-sess.Exited
			Expect(sess.ExitCode()).To(Equal(0))
			Expect(sess.Out).To(gbytes.Say("target saved"))
		})

		AfterEach(func() {
			adminAtcServer.Close()
		})

		Context("with the ATC Url unspecified", func() {
			It("User logs in to any team", func() {
				adminAtcServer.AppendHandlers(
					infoHandler(),
					adminTokenHandler(),
					teamHandler(teams),
				)

				flyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-n", "other-team", "-u", "user", "-p", "pass")

				sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())

				Eventually(sess).Should(gbytes.Say("logging in to team 'other-team'"))

				<-sess.Exited
				Expect(sess.ExitCode()).To(Equal(0))
				Expect(sess.Out.Contents()).To(ContainSubstring("target saved"))
			})
		})

		Context("when the CACert is provided", func() {
			var caCertFilePath string
			var adminAtcServer *ghttp.Server
			BeforeEach(func(){
				adminAtcServer = ghttp.NewUnstartedServer()
				cert, err := tls.X509KeyPair([]byte(serverCert), []byte(serverKey))
				Expect(err).NotTo(HaveOccurred())

				adminAtcServer.HTTPTestServer.TLS = &tls.Config{
					Certificates: []tls.Certificate{cert},
				}
				adminAtcServer.HTTPTestServer.StartTLS()

				adminAtcServer.AppendHandlers(
					infoHandler(),
					adminTokenHandler(),
					teamHandler(teams),
				)

				caCertFile, err := ioutil.TempFile("", "fly-login-test")
				Expect(err).NotTo(HaveOccurred())
				caCertFilePath = caCertFile.Name()

				err = ioutil.WriteFile(caCertFilePath, []byte(serverCert), os.ModePerm)
				Expect(err).NotTo(HaveOccurred())
				setupFlyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-c", adminAtcServer.URL(), "-n", "main", "--ca-cert", caCertFilePath, "-u", "user", "-p", "pass")

				sess, err := gexec.Start(setupFlyCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				<-sess.Exited
				Expect(sess.ExitCode()).To(Equal(0))
			})

			AfterEach(func() {
				os.RemoveAll(caCertFilePath)
				adminAtcServer.Close()
			})

			Context("when ca cert is not provided this time", func() {
				It("is using saved ca cert", func() {
					adminAtcServer.AppendHandlers(
						infoHandler(),
						adminTokenHandler(),
						teamHandler(teams),
					)
					flyCmd := exec.Command(flyPath, "-t", "some-target", "login", "-n", "main", "-u", "user", "-p", "pass")
					sess, err := gexec.Start(flyCmd, GinkgoWriter, GinkgoWriter)
					Expect(err).NotTo(HaveOccurred())

					<-sess.Exited
					Expect(sess.ExitCode()).To(Equal(0))
				})
			})
		})
	})
})
