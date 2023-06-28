#!/bin/sh
uninstall() {
  if type "systemctl" > /dev/null 2>&1; then
    printf "\e[31mstopping the runetale...\e[m\n"
    sudo systemctl stop runetaled.service || true
    if [ -e /lib/systemd/system/runetaled.service  ]; then
      rm -f /lib/systemd/system/runetaled.service 
      systemctl daemon-reload || true
      printf "\e[31mremoved runetaled.service and reloaded daemon.\e[m\n"
    fi
  
  else
    printf "\e[31muninstalling the runetale...\e[m\n"
    /usr/bin/runetaled daemon uninstall || true
  
    if type "systemctl" > /dev/null 2>&1; then
       printf "\e[31mrunning daemon the reload\e[m\n"
       systemctl daemon-reload || true
    fi
  
  fi
}

uninstall
