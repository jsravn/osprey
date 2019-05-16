package cmd

import (
	"github.com/sky-uk/osprey/client"
	"github.com/sky-uk/osprey/client/kubeconfig"
	"github.com/spf13/cobra"

	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to one or more Kubernetes clusters",
	Long: `Login will take the user credentials and validate against the configured set of osprey servers.
On a successful authentication it will generate a kubectl config file that will grant the user access to the clusters
via different contexts.

It expects the osprey server(s) to return OpenID Connect required information to set up the kubectl config auth-provider.
It will generate a kubectl configuration file for the specified server(s).

The connection to the osprey servers is via HTTPS.
`,
	Run: login,
}

var interactive bool

func init() {
	userCmd.AddCommand(loginCmd)
	loginCmd.Flags().BoolVarP(&interactive, "interactive", "i", true, "set to false if not using a GUI (requires copy and pasting of tokens)")
}

func login(_ *cobra.Command, _ []string) {
	ospreyconfig, err := client.LoadConfig(ospreyconfigFile)
	if err != nil {
		log.Fatalf("Failed to load ospreyconfig file %s: %v", ospreyconfigFile, err)
	}

	err = kubeconfig.LoadConfig(ospreyconfig.Kubeconfig)
	if err != nil {
		log.Fatalf("Failed to initialise kubeconfig: %v", err)
	}

	groupName := ospreyconfig.GroupOrDefault(targetGroup)
	snapshot := client.GetSnapshot(ospreyconfig)
	group, ok := snapshot.GetGroup(groupName)
	if !ok {
		log.Errorf("Group not found: %q", groupName)
		os.Exit(1)
	}

	displayActiveGroup(targetGroup, ospreyconfig.DefaultGroup)
	ospreyconfig.Interactive = interactive

	retrieverFactory, err := client.NewFactory(ospreyconfig)
	if err != nil {
		log.Fatalf("unable to initialise providers: %v", err)
	}

	success := true

	for _, target := range group.Targets() {
		retriever, err := retrieverFactory.GetRetriever(target.ProviderType())
		if err != nil {
			log.Fatalf(err.Error())
		}

		tokenData, err := retriever.RetrieveClusterDetailsAndAuthTokens(target)

		if err != nil {
			if state, ok := status.FromError(err); ok && state.Code() == codes.Unauthenticated {
				log.Fatalf("Failed to log in to %s: %v", target.Name(), state.Message())
			}
			success = false
			log.Errorf("Failed to log in to %s: %v", target.Name(), err)
			continue
		}
		updateKubeconfig(err, target, tokenData)
	}

	if !success {
		log.Fatal("Failed to update credentials for some targets.")
	}
}

func updateKubeconfig(err error, target client.Target, tokenData *client.ClusterInfo) {
	err = kubeconfig.UpdateConfig(target.Name(), target.Aliases(), tokenData)
	if err != nil {
		log.Errorf("Failed to update config for %s: %v", target.Name(), err)
	}
	aliases := ""
	if target.HasAliases() {
		aliases = fmt.Sprintf(" | %s", strings.Join(target.Aliases(), " | "))
	}
	log.Infof("Logged in to: %s%s", target.Name(), aliases)
}
