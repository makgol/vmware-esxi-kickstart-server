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
    apt-get -y install powershell && \
    pwsh -c Install-Module VMware.PowerCLI -Force && \
    pip install --no-cache-dir six psutil lxml pyopenssl

FROM python:3.7-slim

WORKDIR /work

RUN  apt-get update && apt-get install -y libicu-dev && \
     apt-get clean && \
     rm -rf /var/lib/apt/lists/*

COPY --from=pybuild /opt/microsoft/ /opt/microsoft/
COPY --from=pybuild /usr/local/lib/python3.7/site-packages/ /usr/local/lib/python3.7/site-packages/
COPY --from=pybuild /root/.local/share/powershell/Modules /root/.local/share/powershell/Modules
COPY --from=gobuild /gobuild/kickstart-server /usr/local/bin/kickstart-server

RUN ln -s /opt/microsoft/powershell/7/pwsh /usr/bin/pwsh

EXPOSE 67/udp

EXPOSE 69/udp

EXPOSE 80

CMD ["kickstart-server"]
