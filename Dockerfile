FROM golang

RUN mkdir -p /go/src/ncloud-api

WORKDIR /go/src/ncloud-api

COPY . /go/src/ncloud-api

RUN mkdir /var/ncloud_upload

RUN chown $(whoami) /var/ncloud_upload/

RUN go install ncloud-api

CMD ["/go/bin/ncloud-api"]

EXPOSE 80

EXPOSE 443

EXPOSE 8080
