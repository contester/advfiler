%global debug_package %{nil}
 
Name:		contester-advfiler
Version:	0.0.1
Release:	1%{?dist}
Summary:	Contester storage
 
License:	MIT
URL:		http://stingr.net
Source0:	%{name}-%{version}.tar.gz
 
BuildRequires:	golang
BuildRequires:  systemd
Requires(pre): shadow-utils
%{?systemd_requires}
 
%description
Installer backend and frontend
 
%pre
getent group %{name} >/dev/null || groupadd -r %{name}
getent passwd %{name} >/dev/null || \
    useradd -r -g %{name} -d %{_sharedstatedir}/%{name} -s /sbin/nologin \
    -c "Smartpxe" %{name}
exit 0
 
%post
%systemd_post %{name}.service %{name}.socket
 
%preun
%systemd_preun %{name}.service %{name}.socket
 
%postun
%systemd_postun_with_restart %{name}.service %{name}.socket
 
%prep
%setup -q
 
%build
mkdir -p goapp/src/git.sgu.ru/sgu goapp/bin
ln -s ${PWD} goapp/src/git.sgu.ru/sgu/%{name}
export GOPATH=${PWD}/goapp
%gobuild -o goapp/bin/smartpxe git.sgu.ru/sgu/%{name}/smartpxe
 
%install
 
%{__install} -d $RPM_BUILD_ROOT%{_bindir}
%{__install} -v -D -t $RPM_BUILD_ROOT%{_bindir} goapp/bin/smartpxe
%{__install} -d $RPM_BUILD_ROOT%{_unitdir}
%{__install} -v -D -t $RPM_BUILD_ROOT%{_unitdir} smartpxe.service
%{__install} -v -D -t $RPM_BUILD_ROOT%{_unitdir} smartpxe.socket
%{__install} -d -m 0755 %{buildroot}%{_sysconfdir}/%{name}
%{__install} -d $RPM_BUILD_ROOT%{_sysconfdir}/sysconfig
%{__install} -m 644 -T smartpxe.sysconfig %{buildroot}%{_sysconfdir}/sysconfig/smartpxe
%{__install} -d -m 0755 %{buildroot}%{_sharedstatedir}/%{name}
 
%files
%{_bindir}/smartpxe
%dir %attr(-,%{name},%{name}) %{_sharedstatedir}/%{name}
%{_unitdir}/smartpxe.service
%{_unitdir}/smartpxe.socket
%config(noreplace) %{_sysconfdir}/%{name}
%config(noreplace) %{_sysconfdir}/sysconfig/smartpxe
 
%changelog
