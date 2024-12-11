package mapperclient

// TODO: set to main before merge
//go:generate wget https://raw.githubusercontent.com/otterize/network-mapper/shavit/reportAWSOperation2/src/mappergraphql/schema.graphql -O ./schema.graphql -q
//go:generate go run github.com/Khan/genqlient@v0.7.0 ./genqlient.yaml
