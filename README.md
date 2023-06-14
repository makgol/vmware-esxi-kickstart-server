# Automated Installation Tool for Nested ESXi with Kickstart 

## Overview
This tool is designed to assist in the automatic installation of Nested ESXi through Kickstart.

## Functionality
When the code is executed, Web&API, DHCP, and TFTP services are initiated.

- **Web&API**
  - The default port number is 80. It processes POST requests containing the information for the ESXi to be deployed, creates ks.cfg, determines the DHCP lease IP corresponding to the ESXi's MAC address, and retains the mapping information. It also maintains the mapping of ks.cfg and the MAC address, responding with the appropriate file such as ks.cfg and ESXi's installer files to GET requests from the target IP. This service also handles the upload of ESXi ISO files. Uploaded ISOs are edited for PXE installation. Additionally, if you install powercli 13.0 or later on your Linux environment, you will also be able to upload upgrade bundle zip files. If an upgrade bundle is uploaded, the server will convert it into an ISO file targeting the standard patch, and perform editing for PXE installation.
- **DHCP**
  - Executes IP address lease for each MAC address according to the mapping information created by the API. The DHCP options include the bootfile of the ESXi version and the information of the TFTP server contained in the POST request received by the API. This DHCP has the ability to automatically determine BIOS, UEFI, UEFI HTTP Boot and respond with the appropriate filename. Also, the discover messages from MAC addresses without mapping information are ignored. By default, the DHCP lease range is set to consider the entire CIDR range of the service port as a valid lease range. DHCP has ability to duplicate check by ARP and ignore unknown discover message as described above, so no harm to existing DHCP networks. However, it is recommended that the network of the service port be dedicated to Nested ESXi.
- **TFTP**
  - Used by iPXE. Respond with bootloader and boot config.

By default, the tool checks the ethernet interfaces on the OS at startup and determines the first interface it finds as the API port and the second as the service port. The following are launched on each interface:

- API port serves
  - API
- Service port serves
  - API, DHCP, TFTP

Also, by default, it creates a directory for file upload (files) and a directory for saving ks.cfg (ks) on the execution directory at startup.

These default settings can be changed using environment variables.

## Requirements
- If using the default settings, prepare a Linux with two interfaces.
- Execution requires root user permissions.
- Both interfaces should have IPv4 addresses assigned.
- The network of the ESXi that boots with PXE must be on the same network as the service port.

## Supported boot protocols and limitation
iPXE and HTTP UEFI Boot are supported. These are automatically determined from the DHCP discover message, so no special tuning is required on the user side. However, the bootloader for iPXE included in this source code is not signed by Microsoft, so secure boot with iPXE will not work. HTTP UEFI boot supports secure boot, but if you want to install 6.x version with secure boot, you need upload any 7.0 or later ESXi iso to the web before doing so. This is a limitation of the boot loader included in the 6.x iso which does not support finding boot configs with relative paths. When upload the iso file to web, web compares Esxi version with the latest ESXi version currently uploaded and updates the bootloader shipped with the latest version iso for HTTP UEFI boot.

| Boot protocols | BIOS Boot | UEFI Boot | UEFI Secure Boot | Notes |
| :--- | :--- | :--- | :--- | :--- |
| iPXE | Supported | Supported | Not Supported ||
| HTTP UEFI Boot | Not Supported | Supported | Supported* | *If installing 6.x with secure boot, need to upload a any 7.x or higher iso in advance to update to the corresponding bootloader. |

## Environment Variables
The default values can be changed by setting the following environment variables:

| Environment variables name | Default value | Notes |
| :--- | :--- | :--- |
| `API_SERVER_PORT` | `80` | Changes the port number of the API. |
| `KS_DIR_PATH` | `./` | Changes the directory where ks.cfg is saved. | 
| `FILE_DIR_PATH` | `./files` | Changes the directory where ISO is uploaded. |
| `LOG_FILE_PATH` | `/var/log/ks-server.log` | Changes the log output file. |
| `SERVICE_IP_ADDR` | The second nic ip address | Starts the service port on the interface with the IP in this variable. |
| `DHCP_START_IP` | The second nic CIDR range first | Sets the start IP of the DHCP lease range. The end IP setting is also required. |
| `DHCP_END_IP` | The second nic CIDR range end| Sets the end IP of the DHCP lease range. The start IP setting is also required. |

## Usage
1. Execute the code.
    ```bash
    go run main.go
    ```

2. Access the Web and upload the ESXi ISO or zip upgrade bundle file.
    ```
    http://<Web&API IP>:<API_SERVER_PORT>
    ```

3. Create a Nested ESXi VM and note down the MAC address of the vnic for PXE boot. Do not start the VM at this point.

4. Send a POST request to the API for the VM created in step 3.

    - **URI**: 
        ```
        POST http://<Web&API IP>:<API_SERVER_PORT>/ks
        ```
    - **Required Headers**: 
        ```
        Content-Type: application/json
        ```
    - **Body**: 
        | Key | Value | Required | Notes |
        | :--- | :--- | :--- | :--- |
        | `macaddress` | string | yes | MAC address of the interface used for PXE boot |
        | `password` | string | yes | Root user password of the Nested ESXi |
        | `ip` | string | yes | IP address of vmk0 |
        | `netmask` | string | yes | Network mask of vmk0 |
        | `gateway` | string | yes | Default gateway of vmk0 |
        | `nameserver` | string | yes | DNS server of vmk0 |
        | `hostname` | string | yes | Hostname of the Nested ESXi |
        | `vlanid` | integer | no | VLAN ID of vmk0. Default value is 0. |
        | `keyboard` | string | no | Keyboard layout of the OS, the default value is English(`US Default`). |
        | `isofilename` | string | yes | Filename of the ISO to be installed. It must have the same name as the uploaded ISO file. |
        | `cli` | array | no | CLI commands to be executed after installation. Please note that these will not work if Secure Boot is enabled. |

    - **Example POST request**:
      ```
      POST http://<Web&API IP>:<API_SERVER_PORT>/ks
      Content-Type: application/json

      {
          "macaddress": "00:50:56:99:c4:74",
          "password": "VMware1!",
          "ip": "192.168.1.1",
          "netmask": "255.255.255.0",
          "gateway": "192.168.1.254",
          "nameserver": "192.168.1.250",
          "hostname": "testesxi001.vsphere.local",
          "vlanid": 11,
          "keyboard": "Japanese",
          "isofilename": "VMware-VMvisor-Installer-7.0U3c-19193900.x86_64.iso",
          "cli": [
              "vim-cmd hostsvc/enable_ssh",
              "vim-cmd hostsvc/start_ssh",
              "vim-cmd hostsvc/enable_esx_shell",
              "vim-cmd hostsvc/start_esx_shell"
          ]
      }
      ```

5. Power on the Nested ESXi VM. The installation will begin automatically.

6. After the installation is complete, send a DELETE request to delete the corresponding mac address and ip address mapping information.

    Example DELETE request:
    ```
    DELETE http://<Web&API IP>:<API_SERVER_PORT>/ks/00-50-56-99-c4-74
    ```

## Getting ESXi versions
You can use the following API to verify the mapping of iso file names to ESXi versions. This is useful for checking uploaded iso files and for deciding the guest_os_version of Nested ESXi and the VDS version to use when deploying a Nested vSphere environment automatically in conjunction with tools like Ansible.

- **URI**:
  ```
  GET http://<Web&API IP>:<API_SERVER_PORT>/esxi-versions
  ```

- **Response Sample**:
  ```
  {
    "uploaded_esxi_list": {
      "VMware-VMvisor-Installer-7.0U3-18644231.x86_64.iso": "7.0.3",
      "VMware-VMvisor-Installer-7.0U3d-19482537.x86_64.iso": "7.0.3",
      "VMware-VMvisor-Installer-7.0U3g-20328353.x86_64.iso": "7.0.3",
      "VMware-VMvisor-Installer-8.0-20513097.x86_64.iso": "8.0.0",
      "VMware-VMvisor-Installer-8.0U1-21495797.x86_64.iso": "8.0.1"
    }
  }
  ```

## Docker support
This tool can also be run as a Docker container.The requirements remain unchanged even when using Docker. It is necessary to run in host network mode. It is recommended when using it in environments where you want to use an upgrade bundle and it is difficult to install PowerCLI to your server.
1. Build the docker image
```
docker build -t kickstart-server .
```

2. Run the docker container
```
docker run --name kickstart-server --restart=always --net=host -v $(pwd)/files:/work/files -itd kickstart-server
```

## Related tools
- vmware-esxi-kickstart-client  
https://github.com/makgol/vmware-esxi-kickstart-client
