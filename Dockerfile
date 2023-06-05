FROM golang:latest as gobuild

WORKDIR /gobuild

COPY . .

RUN go mod download && \
    go build -o kickstart-server main.go

FROM python:3.7-slim as pybuild

WORKDIR /pybuild

RUN apt-get update && \
    apt-get install -y wget apt-transport-https software-properties-common && \
    wget -q "https://packages.microsoft.com/config/debian/$(lsb_release -rs)/packages-microsoft-prod.deb" && \
    dpkg -i packages-microsoft-prod.deb && \
    rm packages-microsoft-prod.deb && \
    apt-get update && \ 
    apt-get -y install powershell


FROM python:3.7-slim

WORKDIR /work

RUN  apt-get update && apt-get install -y libicu-dev

COPY --from=pybuild /opt/microsoft/ /opt/microsoft/

RUN ln -s /opt/microsoft/powershell/7/pwsh /usr/bin/pwsh && \
    pwsh -c Install-Module VMware.PowerCLI -Force && \
    pip install six psutil lxml pyopenssl

COPY --from=gobuild /gobuild/kickstart-server /usr/local/bin/kickstart-server

EXPOSE 67/udp

EXPOSE 69/udp

EXPOSE 80

CMD ["./kickstart-server"]
