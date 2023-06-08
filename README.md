# ncloud-api
## Setup
### Install dependencies:
* golang
* docker
* docker-compose

### Create API key for meilisearch
```
sudo mkdir /var/local/meilisearch
sudo echo 'MEILI_MASTER_KEY="your_api_key"' > /var/local/meilisearch/meilisearch.env
 ```

### Start docker (if not already up):
`systemctl start docker`

### Start container
`docker-compose -f docker-compose.yaml up -d`

### Create directory for upload
```
sudo mkdir /var/ncloud_upload
sudo chown $(whoami) /var/ncloud_upload
```

### Run server
`go run .`
